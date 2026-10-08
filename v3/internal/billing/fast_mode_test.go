package billing

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestFastModeFreezesDoubleReservationAndSettlement(t *testing.T) {
	for _, tier := range []string{"fast", "priority", "default", "auto", "flex", ""} {
		t.Run(tier, func(t *testing.T) {
			req := &gateway.Request{Protocol: gateway.ProtocolResponses, Model: "model", Body: []byte(`{"model":"model","input":"hi","service_tier":"` + tier + `"}`),
				Targets: []gateway.Target{{ChannelID: 1, CredentialID: 11, Provider: "codex", Group: "default", MultiplierPPM: 1_000_000}}}
			snap := &catalog.Snapshot{Channels: map[int64]*catalog.Channel{1: {ID: 1}}, Prices: map[string]catalog.Price{"model": {Mode: "per_request", PerRequest: 100}}}
			s := &Settler{}
			frozen, price, multiplier, err := s.freezeTargetPrices(req, snap)
			if err != nil {
				t.Fatal(err)
			}
			want := credits.Micro(100)
			if tier == "fast" || tier == "priority" {
				want = 200
			}
			amount, err := s.estimateTargetPrices(req, frozen, price, multiplier, pricing.RequestInput{}, nil, nil)
			if err != nil || amount != want {
				t.Fatalf("reservation=%d err=%v; want %d", amount, err, want)
			}
			h := &hold{price: price, multiplier: multiplier, targetPrices: frozen}
			req.Body = []byte(`{"service_tier":"default"}`)
			amount, err = s.settlementPrice(h, gateway.Outcome{Charge: true, Target: &req.Targets[0]})
			if err != nil || amount != want {
				t.Fatalf("frozen settlement=%d err=%v; want %d", amount, err, want)
			}
			amount, err = s.settlementPrice(h, gateway.Outcome{Charge: false, Target: &req.Targets[0]})
			if err != nil || amount != 0 {
				t.Fatalf("failed request charge=%d err=%v", amount, err)
			}
		})
	}
}

func TestFastModeDoesNotMutateCatalogAndSurvivesDurableRestore(t *testing.T) {
	req := &gateway.Request{ID: "fast-durable", Protocol: gateway.ProtocolResponses, Model: "model", Body: []byte(`{"model":"model","input":"hello","service_tier":"fast"}`),
		Principal: gateway.Principal{UserID: 7, KeyID: 70, Group: "default"}, Targets: []gateway.Target{{ChannelID: 1, CredentialID: 11, Group: "default", MultiplierPPM: 1_000_000}}}
	p := catalog.Price{Mode: "per_request", PerRequest: 100, Rules: map[string]any{"money_quantum": 1}}
	snap := &catalog.Snapshot{Channels: map[int64]*catalog.Channel{1: {ID: 1}}, Prices: map[string]catalog.Price{"model": p}}
	s := &Settler{}
	frozen, price, factor, err := s.freezeTargetPrices(req, snap)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Rules[pricing.FastModeRule]; ok {
		t.Fatal("Fast admission mutated the shared catalog")
	}
	h := &hold{account: 42, amount: 200, price: price, multiplier: factor, cardMultiplier: 1, targetPrices: frozen}
	reservation, err := encodeWorkflowHold(req, h)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restoreWorkflowHold(req, reservation)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		tier string
		want credits.Micro
	}{{"fast", 200}, {"priority", 200}, {"", 200}, {"default", 100}} {
		got, err := s.settlementPrice(restored, gateway.Outcome{Charge: true, Target: &req.Targets[0], Usage: gateway.Usage{ServiceTier: tc.tier}})
		if err != nil || got != tc.want {
			t.Fatalf("restored tier=%q amount=%d err=%v; want %d", tc.tier, got, err, tc.want)
		}
	}
}

func TestFastModeKeepsLegacyCardAndSubscriptionRights(t *testing.T) {
	target := gateway.Target{ChannelID: 1, CredentialID: 11, Group: "default"}
	for _, tc := range []struct {
		name       string
		packagePPM int64
		want       credits.Micro
	}{{"current plan", 1_000_000, 160}, {"legacy plan", 10_000_000, 1600}} {
		t.Run(tc.name, func(t *testing.T) {
			h := &hold{targetPrices: map[string]targetPrice{targetPriceKey(target): {Price: catalog.Price{Mode: "per_request", PerRequest: 100, Rules: map[string]any{pricing.FastModeRule: true}}, MultiplierPPM: 1_000_000, SubscriptionAllowed: true, SubscriptionFactorPPM: 1_000_000, PackagePPM: tc.packagePPM}},
				cards: []catalog.MultiplierCard{{ID: 99, MultiplierPPM: 800_000}}, cardChannels: map[int64]bool{1: true}}
			quote, err := (&Settler{}).quoteSource(h, target, gateway.Usage{ServiceTier: "fast"})
			if err != nil || quote.Wallet != 160 || quote.Subscription != tc.want || quote.CardID != 99 {
				t.Fatalf("quote=%+v err=%v", quote, err)
			}
		})
	}
}
