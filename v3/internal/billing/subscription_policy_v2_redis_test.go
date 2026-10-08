//go:build pgintegration

package billing

import (
	"errors"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func standardFixture(t *testing.T, s *Settler, now time.Time) (*gateway.Request, *catalog.Snapshot) {
	t.Helper()
	req, snapshot := sourceFixture(now)
	profile := snapshot.AccountProfiles[7]
	ppm := int64(970873)
	profile.Subscriptions[0].PolicyVersion = "standard_v2"
	profile.Subscriptions[0].SubscriptionID = 13
	profile.Subscriptions[0].OrderID = 130
	profile.Subscriptions[0].RevenueMultiplierPPM = &ppm
	snapshot.AccountProfiles[7] = profile
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	return req, snapshot
}

func TestStandardSubscriptionMatchesWalletAndSkipsLegacyPackageCard(t *testing.T) {
	for _, refund := range []bool{false, true} {
		s, rdb, _, clock := setup(t, 1000)
		req, snapshot := standardFixture(t, s, clock.now())
		profile := snapshot.AccountProfiles[7]
		profile.Cards = []catalog.MultiplierCard{{ID: 91, PropType: "monthly_pass_multiplier", MultiplierPPM: 500000, ExpiresAt: clock.now().Add(time.Hour)},
			{ID: 92, PropType: "consume_discount_90", MultiplierPPM: 900000, ExpiresAt: clock.now().Add(time.Hour)}}
		snapshot.Channels[1].MultiplierCardUserEnabled = true
		snapshot.AccountProfiles[7] = profile
		if err := rdb.HSet(ctx, BalanceKey(43), "balance", 1000, "reserved", 0, "ver", 0).Err(); err != nil {
			t.Fatal(err)
		}
		if err := s.Reserve(ctx, req); err != nil {
			t.Fatal(err)
		}
		h := req.Reserve.(*hold)
		if h.amount != 81 {
			t.Fatalf("standard reserve=%d want wallet81", h.amount)
		}
		if err := s.Finalize(ctx, req, gateway.Outcome{Charge: !refund, Target: &req.Targets[0]}); err != nil {
			t.Fatal(err)
		}
		want := int64(919)
		if refund {
			want = 1000
		}
		if got, _ := rdb.HGet(ctx, BalanceKey(43), "balance").Int64(); got != want {
			t.Fatalf("balance=%d want%d", got, want)
		}
		if bal, held := balance(t, rdb); bal != 1000 || held != 0 {
			t.Fatalf("wallet=%d/%d", bal, held)
		}
		all := events(t, rdb)
		if refund && (len(all) != 1 || all[0]["funding_policy_version"] != nil) {
			t.Fatalf("release masqueraded as consumption=%#v", all)
		}
		if !refund && (len(all) != 1 || all[0][FieldMarketGross] != "81" || all[0]["funding_policy_version"] != "standard_v2" || all[0]["funding_wallet_equivalent_micro"] != "90") {
			t.Fatalf("audit=%#v", all)
		}
	}
}

func TestStandardLegacyWalletMixedFrozenPricingWALAndIdempotency(t *testing.T) {
	s, rdb, _, clock := setup(t, 1000)
	req, snapshot := standardFixture(t, s, clock.now())
	profile := snapshot.AccountProfiles[7]
	profile.Subscriptions = append(profile.Subscriptions, catalog.SubscriptionBucket{AccountID: 45, SubscriptionID: 15, PolicyVersion: "legacy", StartsAt: clock.now().Add(-time.Hour), ExpiresAt: clock.now().Add(time.Hour)})
	snapshot.AccountProfiles[7] = profile
	for id, amount := range map[int64]int64{43: 70, 45: 150} {
		if err := rdb.HSet(ctx, BalanceKey(id), "balance", amount, "reserved", 0, "ver", 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if got := req.Reserve.(*hold).amount; got != 225 {
		t.Fatalf("aggregate=%d want225", got)
	}
	w, err := openWAL(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.wal = w
	t.Cleanup(s.Close)
	for range 3 {
		s.br.fail()
	}
	profile.Subscriptions[0].PolicyVersion = "legacy"
	snapshot.AccountProfiles[7] = profile
	snapshot.Channels[1].Settings["credit_pool_policy"] = "marketplace_universal_only"
	out := gateway.Outcome{Charge: true, Target: &req.Targets[0]}
	for range 2 {
		if err := s.Finalize(ctx, req, out); err != nil {
			t.Fatal(err)
		}
	}
	if len(events(t, rdb)) != 0 {
		t.Fatal("outage wrote events")
	}
	s.snapshot = func() *catalog.Snapshot { return nil }
	if err := s.Replay(ctx); err != nil {
		t.Fatal(err)
	}
	if bal, held := balance(t, rdb); bal != 995 || held != 0 {
		t.Fatalf("wallet=%d/%d", bal, held)
	}
	all := events(t, rdb)
	if len(all) != 3 {
		t.Fatalf("events=%#v", all)
	}
	for _, event := range all {
		if event[FieldMarketGross] != "90" || event["usage_total_amount"] != "225" || event[FieldBillingSource] != "mixed" {
			t.Fatalf("facts=%#v", event)
		}
	}
	if all[0]["funding_wallet_equivalent_micro"] != "70" || all[1]["funding_wallet_equivalent_micro"] != "15" || all[2]["funding_wallet_equivalent_micro"] != "5" {
		t.Fatalf("shares=%#v", all)
	}
}

func TestStandardSubscriptionOnlyCapsRejectAndDurableRecovery(t *testing.T) {
	s, rdb, _, clock := setup(t, 1000)
	req, snapshot := standardFixture(t, s, clock.now())
	profile := snapshot.AccountProfiles[7]
	profile.BillingPreference = "subscription_only"
	profile.Subscriptions[0].ModelLimits = map[string]int64{"model": 80}
	snapshot.AccountProfiles[7] = profile
	if err := rdb.HSet(ctx, BalanceKey(43), "balance", 1000, "reserved", 0, "ver", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, req); !errors.Is(err, gateway.ErrInsufficientCredits) {
		t.Fatalf("cap reject=%v", err)
	}
	if reserved, _ := rdb.HGet(ctx, BalanceKey(43), "reserved").Int64(); reserved != 0 {
		t.Fatalf("partial=%d", reserved)
	}
	profile.Subscriptions[0].ModelLimits = nil
	snapshot.AccountProfiles[7] = profile
	w := NewWorkflowSettler(s)
	reservation, err := w.Reserve(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	s.snapshot = func() *catalog.Snapshot { return nil }
	req.Targets[0].Group = ""
	for range 2 {
		amount, err := w.Finalize(ctx, req, reservation, native.Result{Status: "completed"})
		if err != nil || amount != 90 {
			t.Fatalf("durable=%d %v", amount, err)
		}
	}
	if len(events(t, rdb)) != 1 {
		t.Fatal("durable repeat debited")
	}
}
