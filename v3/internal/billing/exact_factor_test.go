package billing

import (
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestExactMarketAdmissionSettlementAndWALAreFrozen(t *testing.T) {
	req, snap := targetPricingFixture()
	req.Targets = req.Targets[:1]
	req.Targets[0].MultiplierPPM = 0 // fractional PPM has no integral fallback
	req.Targets[0].MultiplierPPMExact = "0.01"
	snap.Market.Channels[1].ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: 9000000000}
	s := &Settler{}
	frozen, first, multiplier, err := s.freezeTargetPrices(req, snap)
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := s.estimateTargetPrices(req, frozen, first, multiplier, pricing.RequestInput{}, nil, nil)
	if err != nil || maximum != 90 {
		t.Fatalf("fractional estimate=%d/%v", maximum, err)
	}
	h := &hold{targetPrices: frozen}
	// Changed routing and owner catalog publication cannot reprice admission.
	req.Targets[0].MultiplierPPMExact = "1000000"
	snap.Market.Channels[1].ModelPrices["model"] = catalog.Price{Mode: "per_request", PerRequest: 1}
	out := gateway.Outcome{Charge: true, Target: &req.Targets[0]}
	actual, err := s.settlementPrice(h, out)
	if err != nil || actual != 90 {
		t.Fatalf("fractional settlement=%d/%v", actual, err)
	}
	rec, err := s.appendMarketCall(walRecord{}, h, out, actual)
	if err != nil || len(rec.Args) != 6 || rec.Args[3] != "0.01" {
		t.Fatalf("exact market WAL=%v/%v", rec, err)
	}
	w, err := openWAL(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.close)
	if err := w.append(rec); err != nil {
		t.Fatal(err)
	}
	segments, err := w.sealed()
	if err != nil || len(segments) != 1 {
		t.Fatalf("sealed WAL=%v/%v", segments, err)
	}
	restored, err := readSegment(segments[0].path)
	if err != nil || len(restored) != 1 || restored[0].Args[3] != "0.01" {
		t.Fatalf("WAL read=%v/%v", restored, err)
	}
}

func TestExactMarketSourcePoliciesDoNotTreatTinyAsFree(t *testing.T) {
	now := time.Now()
	for _, factor := range []string{"0.01", "1e-14"} {
		for _, version := range []string{"legacy", "standard_v2"} {
			t.Run(factor+"/"+version, func(t *testing.T) {
				req, snap := sourceFixture(now)
				req.Targets[0].MultiplierPPM, req.Targets[0].MultiplierPPMExact = 0, factor
				snap.Market.Channels[1].ModelPrices["model"] = catalog.Price{Mode: "per_token", InputPerMTok: 1000000000000000000}
				profile := snap.AccountProfiles[7]
				profile.Subscriptions[0].PolicyVersion = version
				s := &Settler{}
				prices, _, _, err := s.freezeTargetPrices(req, snap)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := freezeSourcePolicies(req, snap, prices, profile, now); err != nil {
					t.Fatal(err)
				}
				p := prices[targetPriceKey(req.Targets[0])]
				if !p.SubscriptionAllowed || !p.SubscriptionAccounts[43] || p.SubscriptionFactorPPMExact == "0" {
					t.Fatalf("tiny positive lost subscription eligibility: %+v", p)
				}
				usage := gateway.Usage{PromptTokens: 1}
				wantWallet := int64(10000)
				if factor == "1e-14" {
					usage.PromptTokens, wantWallet = 1000000000000000, 10000000
				}
				quote, err := s.quoteSource(&hold{targetPrices: prices}, req.Targets[0], usage)
				wantSubscription := wantWallet
				if version == "legacy" {
					wantSubscription *= 10
				}
				if err != nil || int64(quote.Wallet) != wantWallet || int64(quote.Subscription) != wantSubscription {
					t.Fatalf("source=%+v/%v; want wallet=%d subscription=%d", quote, err, wantWallet, wantSubscription)
				}
			})
		}
	}
	req, snap := sourceFixture(now)
	req.Targets[0].MultiplierPPMExact = "0" // exact overrides stale positive int
	s := &Settler{}
	prices, _, _, err := s.freezeTargetPrices(req, snap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := freezeSourcePolicies(req, snap, prices, snap.AccountProfiles[7], now); err != nil || prices[targetPriceKey(req.Targets[0])].SubscriptionAllowed {
		t.Fatalf("true zero acquired subscription eligibility: %v", err)
	}
}

func TestExactMarketWorkflowSnapshotRetainsFactor(t *testing.T) {
	req, snap := targetPricingFixture()
	req.ID = "exact-task"
	req.Targets = req.Targets[:1]
	req.Targets[0].MultiplierPPM, req.Targets[0].MultiplierPPMExact = 0, "1e-57"
	s := &Settler{}
	prices, first, multiplier, err := s.freezeTargetPrices(req, snap)
	if err != nil {
		t.Fatal(err)
	}
	h := &hold{account: 42, amount: 0, price: first, multiplier: multiplier, targetPrices: prices}
	reservation, err := encodeWorkflowHold(req, h)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restoreWorkflowHold(req, reservation)
	if err != nil {
		t.Fatal(err)
	}
	canonical := "0." + strings.Repeat("0", 56) + "1"
	if restored.targetPrices[targetPriceKey(req.Targets[0])].MultiplierPPMExact != canonical {
		t.Fatalf("frozen factor lost: %s", reservation.Data)
	}
	target := req.Targets[0]
	target.MultiplierPPMExact = "1000000"
	target, err = frozenWorkflowTarget(restored, target)
	if err != nil || target.MultiplierPPMExact != canonical {
		t.Fatalf("restored target factor=%q/%v", target.MultiplierPPMExact, err)
	}
	reservation.Data = []byte(strings.Replace(string(reservation.Data), canonical, "NaN", 1))
	if _, err := restoreWorkflowHold(req, reservation); err == nil {
		t.Fatal("invalid durable exact factor accepted")
	}
}

func TestExactMarketAdmissionRejectsMalformedFactor(t *testing.T) {
	req, snap := targetPricingFixture()
	s := &Settler{}
	for _, factor := range []string{"-0.01", "NaN", "1/2"} {
		req.Targets[0].MultiplierPPMExact = factor
		if _, _, _, err := s.freezeTargetPrices(req, snap); err == nil {
			t.Fatalf("invalid admission factor %q accepted", factor)
		}
	}
}
