package billing

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func sourceFixture(now time.Time) (*gateway.Request, *catalog.Snapshot) {
	req, snapshot := targetPricingFixture()
	req.ID = "source-price"
	req.Targets = req.Targets[:1]
	req.Principal.UserID, req.Principal.KeyID = 7, 70
	snapshot.AccountProfiles = map[int64]catalog.AccountProfile{7: {WalletAccountID: 42,
		Subscriptions: []catalog.SubscriptionBucket{{AccountID: 43, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}}}}
	snapshot.Channels[1].Settings = map[string]any{"credit_pool_policy": "marketplace_subscription_and_universal"}
	return req, snapshot
}

func TestSourceQuoteMarketPackageAndConsumption(t *testing.T) {
	now := time.Now()
	for _, item := range []struct {
		name                       string
		packagePPM, consumptionPPM int64
		enabled                    bool
		wallet, subscription       int64
	}{
		{"plain", 1_000_000, 1_000_000, true, 90, 900},
		{"consume", 1_000_000, 900_000, true, 81, 810},
		{"package_skips_consume", 100_000, 900_000, true, 81, 90},
		{"disabled", 100_000, 900_000, false, 90, 900},
		{"zero_consume_package", 100_000, 0, true, 0, 90},
	} {
		t.Run(item.name, func(t *testing.T) {
			req, snap := sourceFixture(now)
			snap.Channels[1].MultiplierCardUserEnabled = item.enabled
			profile := snap.AccountProfiles[7]
			profile.Cards = []catalog.MultiplierCard{{ID: 9, PropType: "monthly_pass_multiplier", MultiplierPPM: item.packagePPM, ExpiresAt: now.Add(time.Hour)}}
			s := &Settler{}
			prices, _, _, err := s.freezeTargetPrices(req, snap)
			if err != nil {
				t.Fatal(err)
			}
			mode, err := freezeSourcePolicies(req, snap, prices, profile, now)
			if err != nil || !mode {
				t.Fatalf("freeze = %v/%v", mode, err)
			}
			h := &hold{sourceMode: true, targetPrices: prices, cardChannels: map[int64]bool{1: item.enabled},
				cards: []catalog.MultiplierCard{{ID: 81, MultiplierPPM: item.consumptionPPM}}}
			quote, err := s.quoteSource(h, req.Targets[0], gateway.Usage{})
			if err != nil || int64(quote.Wallet) != item.wallet || int64(quote.Subscription) != item.subscription {
				t.Fatalf("quote wallet=%d sub=%d err=%v", quote.Wallet, quote.Subscription, err)
			}
		})
	}
}

func TestSourcePolicyOfficialDisabledMissingAndExact(t *testing.T) {
	now := time.Now()
	req, snap := sourceFixture(now)
	delete(snap.Market.Channels, 1)
	snap.SubscriptionPolicies = map[string]catalog.SubscriptionPolicy{"market-one": {Enabled: true, MultiplierPPM: 500_000}}
	s := &Settler{}
	prices, _, _, err := s.freezeTargetPrices(req, snap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := freezeSourcePolicies(req, snap, prices, snap.AccountProfiles[7], now); err != nil {
		t.Fatal(err)
	}
	quote, err := s.quoteSource(&hold{targetPrices: prices}, req.Targets[0], gateway.Usage{})
	if err != nil || quote.Wallet != 10 || quote.Subscription != 50 {
		t.Fatalf("official = %#v %v", quote, err)
	}
	delete(snap.SubscriptionPolicies, "market-one")
	if _, err := freezeSourcePolicies(req, snap, prices, snap.AccountProfiles[7], now); err != nil || prices[targetPriceKey(req.Targets[0])].SubscriptionAllowed {
		t.Fatal("missing official group must disable subscription")
	}
	snap.SubscriptionPolicies["market-one"] = catalog.SubscriptionPolicy{Enabled: true, MultiplierPPM: -1}
	if _, err := freezeSourcePolicies(req, snap, prices, snap.AccountProfiles[7], now); err == nil {
		t.Fatal("invalid enabled source accepted")
	}
	if _, err := sourceScale(credits.Micro(math.MaxInt64), 10, 1); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("overflow=%v", err)
	}
	if value, err := sourceScale(1, 1, 1000); err != nil || value != 1 {
		t.Fatalf("minimum=%d/%v", value, err)
	}
	if value, err := sourceScalePackage(3, 500_000, 1_000_000, 900_000); err != nil || value != 1 {
		t.Fatalf("single package rounding=%d/%v", value, err)
	}
	if value, err := sourceScale(9_007_199_254_740_993, 3, 2); err != nil || value != 13_510_798_882_111_490 {
		t.Fatalf("exact=%d/%v", value, err)
	}
}
