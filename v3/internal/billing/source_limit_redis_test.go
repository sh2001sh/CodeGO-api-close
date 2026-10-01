//go:build pgintegration

package billing

import (
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestSourceModelCapConcurrentAdmissionAndSettlement(t *testing.T) {
	s, rdb, _, clock := setup(t, 10_000)
	_, snapshot := sourceFixture(clock.now())
	profile := snapshot.AccountProfiles[7]
	profile.Subscriptions[0].SubscriptionID = 90
	profile.Subscriptions[0].ModelLimits = map[string]int64{"model": 700}
	profile.Subscriptions[0].ModelUsage = map[string]int64{"model": 200}
	snapshot.AccountProfiles[7] = profile
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	if err := rdb.HSet(ctx, BalanceKey(43), "balance", 10_000, "reserved", 0, "ver", 0).Err(); err != nil {
		t.Fatal(err)
	}
	requests := make([]*gateway.Request, 10)
	var group sync.WaitGroup
	for index := range requests {
		req, _ := sourceFixture(clock.now())
		req.ID = time.Unix(int64(index), 0).Format(time.RFC3339)
		requests[index] = req
		group.Go(func() {
			if err := s.Reserve(ctx, req); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if held, _ := rdb.HGet(ctx, BalanceKey(43), "reserved").Int64(); held != 500 {
		t.Fatalf("concurrent cap held=%d", held)
	}
	for _, req := range requests {
		if err := s.Finalize(ctx, req, gateway.Outcome{Charge: true, Target: &req.Targets[0]}); err != nil {
			t.Fatal(err)
		}
	}
	key := sourceLimits(profile, "model")[43].ModelKey
	if used, _ := rdb.HGet(ctx, BalanceKey(43), key+":used").Int64(); used != 700 {
		t.Fatalf("model usage=%d", used)
	}
	if held, _ := rdb.HGet(ctx, BalanceKey(43), key+":reserved").Int64(); held != 0 {
		t.Fatalf("model reserved=%d", held)
	}
	subDebit := 0
	for _, event := range events(t, rdb) {
		if event["subscription_id"] != nil {
			if event["subscription_id"] != "90" || event["subscription_model_debit"] != "500" {
				t.Fatalf("subscription event=%#v", event)
			}
			subDebit++
		}
	}
	if subDebit != 1 {
		t.Fatalf("sub parts=%d", subDebit)
	}
}

func TestSourceDifferentTargetsHoldEachSourceMaximum(t *testing.T) {
	s, rdb, _, clock := setup(t, 1000)
	req, snapshot := sourceFixture(clock.now())
	req.Targets = append(req.Targets, gateway.Target{ChannelID: 2, CredentialID: 22, Group: "market-two", MultiplierPPM: 1_000_000})
	snapshot.Channels[2].Settings = map[string]any{"credit_pool_policy": "marketplace_universal_only"}
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	if err := rdb.HSet(ctx, BalanceKey(43), "balance", 450, "reserved", 0, "ver", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if _, held := balance(t, rdb); held != 300 {
		t.Fatalf("wallet max held=%d", held)
	}
	if held, _ := rdb.HGet(ctx, BalanceKey(43), "reserved").Int64(); held != 450 {
		t.Fatalf("subscription max held=%d", held)
	}
	if req.Reserve.(*hold).amount != 495 {
		t.Fatalf("scenario aggregate=%d", req.Reserve.(*hold).amount)
	}
	if err := s.Finalize(ctx, req, gateway.Outcome{Charge: true, Target: &req.Targets[1]}); err != nil {
		t.Fatal(err)
	}
	if bal, held := balance(t, rdb); bal != 700 || held != 0 {
		t.Fatalf("selected wallet=%d/%d", bal, held)
	}
	if sub, _ := rdb.HGet(ctx, BalanceKey(43), "balance").Int64(); sub != 450 {
		t.Fatalf("unselected subscription=%d", sub)
	}
}

func TestSourcePaidOnlyAndModelScopeUseFrozenFacts(t *testing.T) {
	now := time.Now()
	req, snapshot := sourceFixture(now)
	delete(snapshot.Market.Channels, 1)
	snapshot.SubscriptionPolicies = map[string]catalog.SubscriptionPolicy{"market-one": {Enabled: true, MultiplierPPM: 1_000_000, PaidOnly: true}}
	s := &Settler{}
	prices, _, _, err := s.freezeTargetPrices(req, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	profile := snapshot.AccountProfiles[7]
	if _, err := freezeSourcePolicies(req, snapshot, prices, profile, now); err != nil {
		t.Fatal(err)
	}
	if prices[targetPriceKey(req.Targets[0])].SubscriptionAccounts[43] {
		t.Fatal("unpaid bucket admitted")
	}
	profile.Subscriptions[0].Paid = true
	profile.Subscriptions[0].Models = []string{"other-model"}
	if _, err := freezeSourcePolicies(req, snapshot, prices, profile, now); err != nil {
		t.Fatal(err)
	}
	if prices[targetPriceKey(req.Targets[0])].SubscriptionAccounts[43] {
		t.Fatal("scoped bucket admitted")
	}
	profile.Subscriptions[0].Models = nil
	profile.Subscriptions[0].ModelLimits = map[string]int64{"other-model": 100}
	if _, err := freezeSourcePolicies(req, snapshot, prices, profile, now); err != nil {
		t.Fatal(err)
	}
	if !prices[targetPriceKey(req.Targets[0])].SubscriptionAccounts[43] {
		t.Fatal("cap map incorrectly treated as whitelist")
	}
}

func TestSourceCapSweeperReleasesReservation(t *testing.T) {
	s, rdb, _, clock := setup(t, 1000)
	req, snapshot := sourceFixture(clock.now())
	profile := snapshot.AccountProfiles[7]
	profile.Subscriptions[0].ModelLimits = map[string]int64{"model": 900}
	snapshot.AccountProfiles[7] = profile
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	key := sourceLimits(profile, "model")[43].ModelKey
	clock.ms.Add(s.cfg.ReservationExpiry.Milliseconds() + 1)
	if _, err := s.SweepExpired(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if held, _ := rdb.HGet(ctx, BalanceKey(43), key+":reserved").Int64(); held != 0 {
		t.Fatalf("swept model held=%d", held)
	}
	if err := s.Finalize(ctx, req, gateway.Outcome{Charge: true, Target: &req.Targets[0]}); err != nil {
		t.Fatal(err)
	}
	if used, _ := rdb.HGet(ctx, BalanceKey(43), key+":used").Int64(); used != 0 {
		t.Fatalf("late model usage=%d", used)
	}
	if wallet, _ := rdb.HGet(ctx, BalanceKey(account), "balance").Int64(); wallet != 910 {
		t.Fatalf("late unreserved service wallet=%d", wallet)
	}
}
