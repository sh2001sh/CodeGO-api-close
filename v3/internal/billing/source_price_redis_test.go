//go:build pgintegration

package billing

import (
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestSourceRedisMixedGrossKeyBudgetAndIdempotency(t *testing.T) {
	s, rdb, _, clock := setup(t, 1000)
	req, snapshot := sourceFixture(clock.now())
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	req.Principal.BudgetLimited, req.Principal.BudgetAccountID = true, 44
	for id, amount := range map[int64]int64{43: 450, 44: 1000} {
		if err := rdb.HSet(ctx, BalanceKey(id), "balance", amount, "reserved", 0, "ver", 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := s.Reserve(ctx, req); err != nil {
			t.Fatal(err)
		}
		if req.Reserve.(*hold).amount != 495 {
			t.Fatalf("aggregate reserve=%d", req.Reserve.(*hold).amount)
		}
	}
	if _, held := balance(t, rdb); held != 45 {
		t.Fatalf("wallet held=%d", held)
	}
	// Pricing/policy publication after admission cannot change any source quote.
	snapshot.Market.Channels[1].ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: 9999}
	snapshot.Channels[1].Settings["credit_pool_policy"] = "marketplace_universal_only"
	out := gateway.Outcome{Charge: true, Delivered: true, Terminal: gateway.TerminalCompleted, Target: &req.Targets[0]}
	for range 2 {
		if err := s.Finalize(ctx, req, out); err != nil {
			t.Fatal(err)
		}
	}
	if bal, held := balance(t, rdb); bal != 955 || held != 0 {
		t.Fatalf("wallet=%d/%d", bal, held)
	}
	for id, want := range map[int64]int64{43: 0, 44: 505} {
		if value, _ := rdb.HGet(ctx, BalanceKey(id), "balance").Int64(); value != want {
			t.Fatalf("account %d=%d want=%d", id, value, want)
		}
	}
	all := events(t, rdb)
	if len(all) != 3 {
		t.Fatalf("events=%#v", all)
	}
	for _, event := range all {
		if event["usage_total_amount"] != "495" || event[FieldMarketGross] != "90" || event[FieldBillingSource] != "mixed" {
			t.Fatalf("facts=%#v", event)
		}
	}
	if marker, _ := rdb.Get(ctx, keysFor(account, req.ID).done).Result(); marker != "495" {
		t.Fatalf("done=%s", marker)
	}
}

func TestSourceRedisPackageAuditAndRefund(t *testing.T) {
	for _, refund := range []bool{false, true} {
		s, rdb, _, clock := setup(t, 1000)
		req, snapshot := sourceFixture(clock.now())
		snapshot.Channels[1].MultiplierCardUserEnabled = true
		profile := snapshot.AccountProfiles[7]
		profile.Cards = []catalog.MultiplierCard{{ID: 81, PropType: "consume_discount_90", MultiplierPPM: 900_000, ExpiresAt: clock.now().Add(s.cfg.ReservationExpiry)},
			{ID: 82, PropType: "monthly_pass_multiplier", MultiplierPPM: 100_000, ExpiresAt: clock.now().Add(s.cfg.ReservationExpiry)}}
		snapshot.AccountProfiles[7] = profile
		s.snapshot = func() *catalog.Snapshot { return snapshot }
		if err := rdb.HSet(ctx, BalanceKey(43), "balance", 45, "reserved", 0, "ver", 0).Err(); err != nil {
			t.Fatal(err)
		}
		if err := s.Reserve(ctx, req); err != nil {
			t.Fatal(err)
		}
		out := gateway.Outcome{Charge: !refund, Delivered: !refund, Target: &req.Targets[0], Terminal: gateway.TerminalCompleted}
		if err := s.Finalize(ctx, req, out); err != nil {
			t.Fatal(err)
		}
		all := events(t, rdb)
		if refund {
			if bal, held := balance(t, rdb); bal != 1000 || held != 0 {
				t.Fatalf("refund=%d/%d", bal, held)
			}
			if len(all) != 1 || all[0][FieldCardID] != nil {
				t.Fatalf("refund audit=%#v", all)
			}
			continue
		}
		if len(all) != 2 || all[0][FieldCardBefore] != "90" || all[0][FieldCardAfter] != "86" || all[0][FieldCardID] != "81" || all[0][FieldMarketGross] != "46" || all[1][FieldCardID] != nil {
			t.Fatalf("package audit=%#v", all)
		}
	}
}

func TestSourceRedisInsufficientNoPartialAndClosedWallet(t *testing.T) {
	for _, closed := range []bool{false, true} {
		s, rdb, _, clock := setup(t, 44)
		req, snapshot := sourceFixture(clock.now())
		s.snapshot = func() *catalog.Snapshot { return snapshot }
		if err := rdb.HSet(ctx, BalanceKey(43), "balance", 450, "reserved", 0, "ver", 0).Err(); err != nil {
			t.Fatal(err)
		}
		if closed {
			if err := rdb.HSet(ctx, BalanceKey(account), "balance", 1000, "reserved", 0, "ver", 0, "closed", 1).Err(); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Reserve(ctx, req); !errors.Is(err, gateway.ErrInsufficientCredits) {
			t.Fatalf("admit=%v", err)
		}
		for _, id := range []int64{account, 43} {
			if held, _ := rdb.HGet(ctx, BalanceKey(id), "reserved").Int64(); held != 0 {
				t.Fatalf("partial hold account%d=%d", id, held)
			}
		}
	}
}

func TestSourceRedisUniversalOnlySkipsSubscription(t *testing.T) {
	s, rdb, _, clock := setup(t, 1000)
	req, snapshot := sourceFixture(clock.now())
	snapshot.Channels[1].Settings["credit_pool_policy"] = "marketplace_universal_only"
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if _, held := balance(t, rdb); held != 90 {
		t.Fatalf("wallet=%d", held)
	}
	if held, _ := rdb.HGet(ctx, BalanceKey(43), "reserved").Int64(); held != 0 {
		t.Fatalf("universal sub=%d", held)
	}
	if err := s.Finalize(ctx, req, gateway.Outcome{Charge: true, Target: &req.Targets[0]}); err != nil {
		t.Fatal(err)
	}
	if all := events(t, rdb); len(all) != 1 || all[0][FieldBillingSource] != "wallet" {
		t.Fatalf("wallet source=%#v", all)
	}
}
