package billing

import (
	"fmt"
	"math/big"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/exactfactor"
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
	legacyActive := false
	for _, bucket := range profile.Subscriptions {
		if bucket.PolicyVersion != "standard_v2" && !now.Before(bucket.StartsAt) && now.Before(bucket.ExpiresAt) {
			legacyActive = true
		}
	}
	pref, err := normalizeFundingPreference(profile.BillingPreference, profile.FundingSourceOrder)
	if err != nil {
		return false, err
	}
	for _, target := range req.Targets {
		key := targetPriceKey(target)
		value := prices[key]
		factorText, err := value.exactMultiplier()
		if err != nil {
			return false, fmt.Errorf("billing: invalid frozen source multiplier: %w", err)
		}
		value.PackagePPM = 1_000_000
		value.SubscriptionFactorPPMExact = ""
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
			if factorText == "0" {
				prices[key] = value
				continue
			}
			value.SubscriptionFactorPPM, value.SubscriptionFactorPPMExact, value.SubscriptionAllowed = value.MultiplierPPM, factorText, true
			if legacyActive {
				factor, err := exactfactor.ParsePPM(factorText)
				if err != nil {
					return false, err
				}
				factor.Mul(factor, big.NewRat(10, 1))
				value.SubscriptionFactorPPMExact = exactfactor.Decimal(factor)
				// Nonintegral or large factors live only in the authoritative text.
				value.SubscriptionFactorPPM, _ = exactfactor.Int64(value.SubscriptionFactorPPMExact)
			}
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
		value.SubscriptionPolicies = make(map[int64]subscriptionPrice)
		for _, bucket := range profile.Subscriptions {
			if bucket.PolicyVersion != "" && bucket.PolicyVersion != "legacy" && bucket.PolicyVersion != "standard_v2" {
				return false, fmt.Errorf("billing: invalid subscription policy version")
			}
			if value.SubscriptionAllowed && !now.Before(bucket.StartsAt) && now.Before(bucket.ExpiresAt) && subscriptionBucketAllows(bucket, req.Model, paidOnly) {
				value.SubscriptionAccounts[bucket.AccountID] = true
				version := bucket.PolicyVersion
				if version == "" {
					version = "legacy"
				}
				var revenue *int64
				if bucket.RevenueMultiplierPPM != nil {
					frozen := *bucket.RevenueMultiplierPPM
					revenue = &frozen
				}
				value.SubscriptionPolicies[bucket.AccountID] = subscriptionPrice{PolicyVersion: version, OrderID: bucket.OrderID, RevenueMultiplierPPM: revenue}
			}
		}
		if !legacyActive {
			value.SubscriptionFactorPPM = value.MultiplierPPM
			value.SubscriptionFactorPPMExact = factorText
			if factorText == "0" {
				value.SubscriptionFactorPPM = 1_000_000
				value.SubscriptionFactorPPMExact = "1000000"
			}
			value.PackagePPM = 1_000_000
		}
		prices[key] = value
	}
	return true, nil
}
