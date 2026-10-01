package billing

import (
	"fmt"
	"math/big"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func officialSubscriptionPolicy(snap *catalog.Snapshot, group string) (catalog.SubscriptionPolicy, int64, error) {
	policy, exists := snap.SubscriptionPolicies[group]
	if !exists {
		// V2 treats an unknown/explicitly omitted official group as disabled.
		return catalog.SubscriptionPolicy{}, 0, nil
	}
	if !policy.Enabled {
		return policy, 0, nil
	}
	if policy.MultiplierPPM <= 0 {
		return policy, 0, fmt.Errorf("billing: invalid subscription policy multiplier")
	}
	return policy, policy.MultiplierPPM, nil
}

func subscriptionBucketAllows(bucket catalog.SubscriptionBucket, model string, paidOnly bool) bool {
	if paidOnly && !bucket.Paid {
		return false
	}
	if len(bucket.Models) == 0 {
		return true
	}
	for _, allowed := range bucket.Models {
		if allowed == model || allowed == "*" {
			return true
		}
	}
	return false
}

func marketCreditPolicy(snap *catalog.Snapshot, target gateway.Target) string {
	if policy := snap.Market.Channels[target.ChannelID].CreditPolicy; policy != "" {
		return policy
	}
	channel := snap.Channels[target.ChannelID]
	if channel != nil {
		for _, key := range []string{"credit_pool_policy", "credit_policy"} {
			if value, ok := channel.Settings[key].(string); ok {
				return value
			}
		}
	}
	return ""
}

func freezeSourcePolicies(req *gateway.Request, snap *catalog.Snapshot, prices map[string]targetPrice, profile catalog.AccountProfile, now time.Time) (bool, error) {
	if len(prices) == 0 || len(profile.SubscriptionAccounts(now)) == 0 {
		return false, nil
	}
	monthly, hasPackage := profile.PackageCard("monthly_pass_multiplier", now)
	pref, err := normalizeFundingPreference(profile.BillingPreference, profile.FundingSourceOrder)
	if err != nil {
		return false, err
	}
	for _, target := range req.Targets {
		key := targetPriceKey(target)
		value := prices[key]
		value.PackagePPM = 1_000_000
		if pref == "wallet_only" {
			prices[key] = value
			continue
		}
		paidOnly := false
		if value.Market {
			switch marketCreditPolicy(snap, target) {
			case "marketplace_universal_only", "universal_only":
				prices[key] = value
				continue
			case "marketplace_subscription_and_universal", "subscription_and_universal":
			default:
				return false, fmt.Errorf("billing: market source credit policy missing")
			}
			if value.MultiplierPPM == 0 {
				prices[key] = value
				continue
			}
			factor := new(big.Int).Mul(big.NewInt(value.MultiplierPPM), big.NewInt(10))
			if !factor.IsInt64() {
				return false, fmt.Errorf("billing: market subscription factor overflow")
			}
			value.SubscriptionFactorPPM, value.SubscriptionAllowed = factor.Int64(), true
		} else {
			policy, factor, err := officialSubscriptionPolicy(snap, target.Group)
			if err != nil {
				return false, err
			}
			value.SubscriptionFactorPPM, value.SubscriptionAllowed, paidOnly = factor, policy.Enabled, policy.PaidOnly
		}
		channel := snap.Channels[target.ChannelID]
		if hasPackage && channel != nil && channel.MultiplierCardUserEnabled {
			if monthly.MultiplierPPM <= 0 || monthly.MultiplierPPM > 1_000_000 {
				return false, fmt.Errorf("billing: invalid monthly package multiplier")
			}
			value.PackagePPM = monthly.MultiplierPPM
		}
		value.SubscriptionAccounts = make(map[int64]bool)
		for _, bucket := range profile.Subscriptions {
			if value.SubscriptionAllowed && !now.Before(bucket.StartsAt) && now.Before(bucket.ExpiresAt) && subscriptionBucketAllows(bucket, req.Model, paidOnly) {
				value.SubscriptionAccounts[bucket.AccountID] = true
			}
		}
		prices[key] = value
	}
	return true, nil
}
