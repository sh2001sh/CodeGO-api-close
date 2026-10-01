package billing

import (
	"errors"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func targetPricingFixture() (*gateway.Request, *catalog.Snapshot) {
	req := &gateway.Request{Model: "model", Body: []byte(`{"n":2}`), Principal: gateway.Principal{Group: "pool"}, Targets: []gateway.Target{
		{ChannelID: 1, CredentialID: 11, Group: "market-one", MultiplierPPM: 100_000},
		{ChannelID: 2, CredentialID: 22, Group: "market-two", MultiplierPPM: 1_000_000},
	}}
	snapshot := &catalog.Snapshot{Channels: map[int64]*catalog.Channel{1: {ID: 1}, 2: {ID: 2}},
		Prices: map[string]catalog.Price{"model": {Mode: "per_request", PerRequest: 100}},
		Market: catalog.MarketSnapshot{Channels: map[int64]catalog.MarketChannelPolicy{
			1: {ModelPrices: map[string]catalog.Price{"model": {Mode: "per_request", PerRequest: 900}}},
			2: {ModelPrices: map[string]catalog.Price{"model": {Mode: "per_request", PerRequest: 300}}},
		}}}
	return req, snapshot
}

// Regression: route pools and cross-group retries do not share the original
// API key's global price/group. Reserve the maximum, settle the actual target.
func TestFrozenOwnerPricesWithoutGlobalModelOrPrincipalGroup(t *testing.T) {
	req, snapshot := targetPricingFixture()
	snapshot.Prices = nil
	s := &Settler{}
	frozen, first, multiplier, err := s.freezeTargetPrices(req, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !frozen[targetPriceKey(req.Targets[0])].Market || !frozen[targetPriceKey(req.Targets[1])].Market {
		t.Fatal("owner market presence was not frozen")
	}
	maximum, err := s.estimateTargetPrices(req, frozen, first, multiplier, pricing.RequestInput{}, nil, nil)
	if err != nil || maximum != 300 {
		t.Fatalf("max reserve = %d, %v; want 300", maximum, err)
	}
	h := &hold{price: first, multiplier: multiplier, targetPrices: frozen}
	for index, want := range []credits.Micro{90, 300} {
		got, err := s.settlementPrice(h, gateway.Outcome{Charge: true, Target: &req.Targets[index]})
		if err != nil || got != want {
			t.Fatalf("target %d = %d, %v; want %d", index, got, err, want)
		}
	}
	// New publication and a changed outcome multiplier cannot reprice a hold.
	snapshot.Market.Channels[1].ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: 9999}
	req.Targets[0].MultiplierPPM = 5_000_000
	got, err := s.settlementPrice(h, gateway.Outcome{Charge: true, Target: &req.Targets[0]})
	if err != nil || got != 90 {
		t.Fatalf("price rotated = %d, %v; want frozen 90", got, err)
	}
}

func TestTargetFactorAndGlobalFallback(t *testing.T) {
	req, snapshot := targetPricingFixture()
	delete(snapshot.Market.Channels, 1)
	req.Targets[0].MultiplierPPM = 2_000_000
	s := &Settler{}
	frozen, _, _, err := s.freezeTargetPrices(req, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if frozen[targetPriceKey(req.Targets[0])].Market {
		t.Fatal("official global fallback acquired market classification")
	}
	got, err := s.targetCharge(&hold{targetPrices: frozen}, gateway.Outcome{Target: &req.Targets[0]})
	if err != nil || got != 200 {
		t.Fatalf("global per-target factor = %d, %v; want 200", got, err)
	}
	req.Targets[0].MultiplierPPM = 0
	frozen, _, _, err = s.freezeTargetPrices(req, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.targetCharge(&hold{targetPrices: frozen}, gateway.Outcome{Target: &req.Targets[0]})
	if err != nil || got != 0 {
		t.Fatalf("explicit zero factor = %d, %v; want 0", got, err)
	}
	req.Targets[0].MultiplierPPM = 9_007_199_254_740_993
	snapshot.Prices["model"] = catalog.Price{Mode: "per_request", PerRequest: 1_000_000}
	frozen, _, _, err = s.freezeTargetPrices(req, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.targetCharge(&hold{targetPrices: frozen}, gateway.Outcome{Target: &req.Targets[0]})
	if err != nil || got != 9_007_199_254_740_993 {
		t.Fatalf("exact compiled ppm = %d, %v", got, err)
	}
}

func TestTargetMediaAndConsumptionCard(t *testing.T) {
	req, snapshot := targetPricingFixture()
	snapshot.Market.Channels[1].ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: 40_000, Rules: map[string]any{"billing_unit": "image"}}
	snapshot.Market.Channels[2].ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: 25_000, Rules: map[string]any{"billing_unit": "image"}}
	req.Targets[0].MultiplierPPM = 500_000
	cards := activeCards([]catalog.MultiplierCard{
		{ID: 1, PropType: "monthly_pass_multiplier", MultiplierPPM: 100_000, ExpiresAt: time.Now().Add(time.Hour)},
		{ID: 2, PropType: "consume_discount_90", MultiplierPPM: 900_000, ExpiresAt: time.Now().Add(time.Hour)},
	}, time.Now())
	eligible := map[int64]bool{1: true}
	s := &Settler{}
	frozen, first, multiplier, err := s.freezeTargetPrices(req, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := s.estimateTargetPrices(req, frozen, first, multiplier, pricing.RequestInput{}, cards, eligible)
	if err != nil || maximum != 50_000 {
		t.Fatalf("media max with eligible card = %d, %v; want 50000", maximum, err)
	}
	h := &hold{targetPrices: frozen, cards: cards, cardChannels: eligible}
	out := gateway.Outcome{Charge: true, Target: &req.Targets[0], Usage: gateway.Usage{ImageCount: 2}}
	got, err := s.settlementPrice(h, out)
	if err != nil || got != 36_000 {
		t.Fatalf("actual channel/card charge = %d, %v; want 36000", got, err)
	}
	rec, err := s.appendCardCall(walRecord{}, h, out)
	if err != nil || len(rec.Args) != 6 || rec.Args[1] != "2" || rec.Args[3] != "40000" || rec.Args[5] != "36000" {
		t.Fatalf("actual channel/card event = %#v, %v", rec.Args, err)
	}
	out.Target = &req.Targets[1]
	got, err = s.settlementPrice(h, out)
	if err != nil || got != 50_000 {
		t.Fatalf("unsupported channel/card charge = %d, %v; want 50000", got, err)
	}
}

func TestTargetPricingRejectsInvalidAndUnadmittedTargets(t *testing.T) {
	s := &Settler{}
	for _, mutate := range []func(*gateway.Request, *catalog.Snapshot){
		func(r *gateway.Request, _ *catalog.Snapshot) { r.Targets[0].ChannelID = 99 },
		func(r *gateway.Request, _ *catalog.Snapshot) { r.Targets[0].CredentialID = 0 },
		func(r *gateway.Request, _ *catalog.Snapshot) { r.Targets[0].MultiplierPPM = -1 },
		func(r *gateway.Request, _ *catalog.Snapshot) { r.Targets[0].Group = "" },
		func(r *gateway.Request, _ *catalog.Snapshot) { r.Targets[0].Group = ""; r.Targets[1].Group = "" },
		func(r *gateway.Request, _ *catalog.Snapshot) { r.Targets[1] = r.Targets[0] },
		func(_ *gateway.Request, c *catalog.Snapshot) {
			delete(c.Market.Channels[1].ModelPrices, "model")
			c.Market.Channels[1].ModelPrices["other"] = catalog.Price{}
		},
		func(_ *gateway.Request, c *catalog.Snapshot) {
			c.Market.Channels[1].ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: -1}
		},
	} {
		req, snapshot := targetPricingFixture()
		mutate(req, snapshot)
		if _, _, _, err := s.freezeTargetPrices(req, snapshot); !errors.Is(err, gateway.ErrBillingUnavailable) {
			t.Fatalf("invalid target = %v", err)
		}
	}
	req, snapshot := targetPricingFixture()
	frozen, _, _, err := s.freezeTargetPrices(req, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	h := &hold{targetPrices: frozen}
	if _, err := s.settlementPrice(h, gateway.Outcome{Charge: true}); err == nil {
		t.Fatal("charged a missing target")
	}
	missing := req.Targets[0]
	missing.CredentialID++
	if _, err := s.settlementPrice(h, gateway.Outcome{Charge: true, Target: &missing}); err == nil {
		t.Fatal("charged an unadmitted credential")
	}
	if got, err := s.settlementPrice(h, gateway.Outcome{}); err != nil || got != 0 {
		t.Fatalf("refund without target = %d, %v", got, err)
	}
}
