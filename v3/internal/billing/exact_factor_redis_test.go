//go:build pgintegration

package billing

import (
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestExactMarketReserveFinalizeRedisEvent(t *testing.T) {
	for _, factor := range []string{"0.01", "1e-14", "0"} {
		t.Run(factor, func(t *testing.T) {
			s, rdb, _, _ := setup(t, 100000000)
			req, snapshot := targetPricingFixture()
			req.ID, req.Principal.UserID = "exact-redis", 7
			req.Targets = req.Targets[:1]
			req.Targets[0].MultiplierPPM, req.Targets[0].MultiplierPPMExact = 0, factor
			price := catalog.Price{Mode: "per_request", PerRequest: 9000000000}
			usage := gateway.Usage{}
			want := int64(90)
			if factor == "1e-14" {
				price = catalog.Price{Mode: "per_token", InputPerMTok: 1000000000000000000, OutputPerMTok: 1000000000000000000}
				req.Body = []byte(`{"max_tokens":1000000000000000}`)
				usage.CompletionTokens, want = 1000000000000000, 10000000
			} else if factor == "0" {
				want = 0
			}
			snapshot.Market.Channels[1].ModelPrices["model"] = price
			s.snapshot = func() *catalog.Snapshot { return snapshot }
			if err := s.Reserve(ctx, req); err != nil {
				t.Fatal(err)
			}
			_, reserved := balance(t, rdb)
			if factor == "0.01" && reserved != want {
				t.Fatalf("exact reservation=%d; want %d", reserved, want)
			}
			if factor == "1e-14" && reserved != want {
				t.Fatalf("tiny reservation=%d; want %d", reserved, want)
			}
			// Both mutable inputs are deliberately changed after admission.
			req.Targets[0].MultiplierPPMExact = "1000000"
			snapshot.Market.Channels[1].ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: 1}
			out := gateway.Outcome{Terminal: gateway.TerminalCompleted, Delivered: true, Charge: true, Usage: usage, Target: &req.Targets[0]}
			if err := s.Finalize(ctx, req, out); err != nil {
				t.Fatal(err)
			}
			if err := s.Finalize(ctx, req, out); err != nil {
				t.Fatalf("idempotent exact finalize: %v", err)
			}
			gotBalance, held := balance(t, rdb)
			if gotBalance != 100000000-want || held != 0 {
				t.Fatalf("balance=%d held=%d; want %d/0", gotBalance, held, 100000000-want)
			}
			messages := events(t, rdb)
			if len(messages) != 1 {
				t.Fatalf("exact event count=%d", len(messages))
			}
			frozen := messages[0][FieldMarketMultiplier].(string)
			if frozen != req.Reserve.(*hold).targetPrices[targetPriceKey(req.Targets[0])].MultiplierPPMExact {
				t.Fatalf("Redis event lost frozen factor: %q", frozen)
			}
			if factor == "1e-14" && frozen == "0" {
				t.Fatal("tiny positive Redis event became free")
			}
		})
	}
}

func TestExactMarketRedisReservationRejectsInsufficientBalance(t *testing.T) {
	s, rdb, _, _ := setup(t, 89)
	req, snapshot := targetPricingFixture()
	req.ID, req.Principal.UserID = "exact-denied", 7
	req.Targets = req.Targets[:1]
	req.Targets[0].MultiplierPPM, req.Targets[0].MultiplierPPMExact = 0, "0.01"
	snapshot.Market.Channels[1].ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: 9000000000}
	s.snapshot = func() *catalog.Snapshot { return snapshot }
	if err := s.Reserve(ctx, req); !errors.Is(err, gateway.ErrInsufficientCredits) {
		t.Fatalf("tiny positive insufficient reservation=%v", err)
	}
	got, reserved := balance(t, rdb)
	if got != 89 || reserved != 0 || len(events(t, rdb)) != 0 {
		t.Fatalf("denied reservation wrote money: balance=%d reserved=%d", got, reserved)
	}
}
