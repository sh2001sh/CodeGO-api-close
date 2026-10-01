package billing

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// Literal old-quota vectors were evaluated against v2 session.go:scaleQuota,
// quota_discount.go's integer Math.Round and relay_billing.go's gross round.
// They exercise the actual old integer ingress BEFORE the ×2 conversion.
func TestSourceSameUsageV2QuotaParity(t *testing.T) {
	for _, row := range []struct {
		name                                                                     string
		oldPrice, walletFactor, subscriptionFactor, packageFactor, consumeFactor int64
		market                                                                   bool
		oldWallet, oldSubscription                                               int64
	}{
		{"positive_old_minimum", 1, 1_000_000, 100_000, 1_000_000, 1_000_000, false, 1, 1},
		{"official_distinct_source", 3, 500_000, 500_000, 900_000, 900_000, false, 2, 2},
		{"market_consume", 303, 1_000_000, 10_000_000, 1_000_000, 100_000, true, 30, 303},
		{"market_monthly_bypass", 303, 1_000_000, 10_000_000, 100_000, 900_000, true, 273, 303},
		{"market_round_before_source", 101, 300_000, 3_000_000, 100_000, 900_000, true, 27, 30},
	} {
		t.Run(row.name, func(t *testing.T) {
			target := gateway.Target{ChannelID: 1, CredentialID: 11, Group: "source"}
			h := &hold{targetPrices: map[string]targetPrice{targetPriceKey(target): {
				Price:         catalog.Price{Mode: "per_request", PerRequest: row.oldPrice * 2, Rules: map[string]any{"money_quantum": 2}},
				MultiplierPPM: row.walletFactor, Market: row.market, SubscriptionAllowed: true,
				SubscriptionFactorPPM: row.subscriptionFactor, PackagePPM: row.packageFactor}},
				cards: []catalog.MultiplierCard{{ID: 9, MultiplierPPM: row.consumeFactor}}, cardChannels: map[int64]bool{1: true}}
			quote, err := (&Settler{}).quoteSource(h, target, gateway.Usage{})
			if err != nil || int64(quote.Wallet) != row.oldWallet*2 || int64(quote.Subscription) != row.oldSubscription*2 {
				t.Fatalf("v2×2 wallet/sub want%d/%d got%d/%d err%v", row.oldWallet*2, row.oldSubscription*2, quote.Wallet, quote.Subscription, err)
			}
		})
	}
	if got := consumptionChargeQuantum(6, 100_000, 2); got != 0 {
		t.Fatalf("v2old3×0.1 rounds0, converted debit=%d", got)
	}
	if got, err := sourceScaleQuantum(2, 100_000, 1_000_000, 1_000_000, 2); err != nil || got != 2 {
		t.Fatalf("v2old minimum=%d/%v", got, err)
	}
}
