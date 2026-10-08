//go:build pgintegration

package billing

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestStandardLargeBalanceDoesNotRequireLegacyTenfoldCapacity(t *testing.T) {
	s, rdb, _, clock := setup(t, 1000)
	req, snapshot := standardFixture(t, s, clock.now())
	const amount int64 = 9_007_199_254_740_993
	req.Targets[0].MultiplierPPM = 1_000_000
	req.Targets[0].RoutePoolID = 17
	req.Targets[0].ProcurementCostMultiplier = "0.9"
	policy := snapshot.Market.Channels[1]
	policy.ModelPrices = map[string]catalog.Price{"model": {Mode: "per_request", PerRequest: amount}}
	snapshot.Market.Channels[1] = policy
	if err := rdb.HSet(ctx, BalanceKey(43), "balance", amount+7, "reserved", 0, "ver", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	// Original snapshot storage may be replaced or mutated; this hold's facts
	// must remain values rather than aliases into the published object.
	*snapshot.AccountProfiles[7].Subscriptions[0].RevenueMultiplierPPM = 0
	if err := s.Finalize(ctx, req, gateway.Outcome{Charge: true, Target: &req.Targets[0]}); err != nil {
		t.Fatal(err)
	}
	if got, _ := rdb.HGet(ctx, BalanceKey(43), "balance").Int64(); got != 7 {
		t.Fatalf("large balance=%d", got)
	}
	all := events(t, rdb)
	if len(all) != 1 || all[0][FieldMarketGross] != "9007199254740993" || all[0]["funding_revenue_multiplier_ppm"] != "970873" || all[0]["funding_procurement_cost_micro"] != "8106479329266894" {
		t.Fatalf("large immutable facts=%#v", all)
	}
}

func TestStandardSourceOnlyShortfallRetainsEntireProviderCost(t *testing.T) {
	s, rdb, _, clock := setup(t, 1000)
	req, snapshot := standardFixture(t, s, clock.now())
	req.Body = []byte(`{"n":1}`)
	req.Targets[0].RoutePoolID = 17
	req.Targets[0].ProcurementCostMultiplier = "0.1"
	policy := snapshot.Market.Channels[1]
	policy.ModelPrices = map[string]catalog.Price{"model": {Mode: "per_request", PerRequest: 900, Rules: map[string]any{"billing_unit": "image"}}}
	snapshot.Market.Channels[1] = policy
	profile := snapshot.AccountProfiles[7]
	profile.BillingPreference = "subscription_only"
	snapshot.AccountProfiles[7] = profile
	if err := rdb.HSet(ctx, BalanceKey(43), "balance", 100, "reserved", 0, "ver", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, req, gateway.Outcome{Charge: true, Target: &req.Targets[0], Usage: gateway.Usage{ImageCount: 2}}); err != nil {
		t.Fatal(err)
	}
	if bal, held := balance(t, rdb); bal != 1000 || held != 0 {
		t.Fatalf("forbidden wallet=%d/%d", bal, held)
	}
	all := events(t, rdb)
	if len(all) != 1 || all[0]["source_shortfall_micro"] != "80" || all[0]["funding_procurement_cost_micro"] != "180" {
		t.Fatalf("unfunded service cost lost=%#v", all)
	}
}
