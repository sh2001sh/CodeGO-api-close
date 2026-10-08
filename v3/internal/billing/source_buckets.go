package billing

import (
	"fmt"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Only new admissions use the extended protocol. Frozen v1 WALs retain their
// original tuple layout and arithmetic forever.
func sourceProtocol(h *hold) string {
	for _, target := range h.targetPrices {
		for _, policy := range target.SubscriptionPolicies {
			if policy.PolicyVersion == "standard_v2" {
				return "source-v2"
			}
		}
	}
	return "source-v1"
}

func bucketQuote(p targetPrice, quote sourceQuote, account int64) (credits.Micro, credits.Micro, int64, subscriptionPrice) {
	policy := p.SubscriptionPolicies[account]
	if policy.PolicyVersion == "standard_v2" {
		return quote.Wallet, quote.WalletBefore, 1, policy
	}
	if policy.PolicyVersion == "" {
		policy.PolicyVersion = "legacy"
	}
	return quote.Subscription, quote.SubscriptionBefore, quote.GrossDivisor, policy
}

func frozenRevenue(policy subscriptionPrice) (string, error) {
	if policy.RevenueMultiplierPPM == nil {
		return "", nil
	}
	if *policy.RevenueMultiplierPPM < 0 {
		return "", fmt.Errorf("billing: negative frozen revenue")
	}
	return fmt.Sprint(*policy.RevenueMultiplierPPM), nil
}
