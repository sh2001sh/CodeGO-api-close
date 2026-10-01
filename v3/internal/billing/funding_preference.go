package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const FieldFundingPreference = "funding_preference"

func sourceAllows(h *hold, p targetPrice, account int64) bool {
	if len(h.targetPrices) == 0 {
		return account != h.account && account != h.budgetAccount
	}
	return p.SubscriptionAccounts[account]
}

// Resolvers publish this value off request beside their funding membership.
type FundingPreferenceResolver interface {
	FundingPreference(context.Context, int64) (string, error)
}

type FundingSelectionResolver interface {
	FundingSelection(context.Context, int64) (string, []int64, error)
}

func normalizeFundingPreference(value string, order []string) (string, error) {
	switch value {
	case "", "subscription_first", "wallet_first", "subscription_only", "wallet_only":
	default:
		return "", fmt.Errorf("billing: invalid funding preference")
	}
	if len(order) > 0 {
		if len(order) > 2 || (order[0] != "subscription" && order[0] != "wallet") || (len(order) == 2 && (order[1] == order[0] || (order[1] != "subscription" && order[1] != "wallet"))) {
			return "", fmt.Errorf("billing: invalid funding source order")
		}
		if len(order) == 1 {
			value = order[0] + "_only"
		} else {
			value = order[0] + "_first"
		}
	}
	if value == "" {
		return "subscription_first", nil
	}
	switch value {
	case "subscription_first", "wallet_first", "subscription_only", "wallet_only":
		return value, nil
	default:
		return "", fmt.Errorf("billing: invalid funding preference")
	}
}

func orderedProfileSources(profile catalog.AccountProfile, now time.Time) []int64 {
	active := profile.SubscriptionAccounts(now)
	byID := make(map[int64]int64, len(profile.Subscriptions))
	for _, bucket := range profile.Subscriptions {
		if !now.Before(bucket.StartsAt) && now.Before(bucket.ExpiresAt) {
			byID[bucket.SubscriptionID] = bucket.AccountID
		}
	}
	result, seen := make([]int64, 0, len(active)), make(map[int64]bool)
	for _, id := range profile.SubscriptionOrderIDs {
		if account := byID[id]; account > 0 && !seen[account] {
			result = append(result, account)
			seen[account] = true
		}
	}
	for _, account := range active {
		if !seen[account] {
			result = append(result, account)
			seen[account] = true
		}
	}
	return result
}

func (s *Settler) selectFunding(ctx context.Context, req *gateway.Request, snap *catalog.Snapshot, prices map[string]targetPrice, profile catalog.AccountProfile, now time.Time) (string, []int64, bool, error) {
	pref, err := normalizeFundingPreference(profile.BillingPreference, profile.FundingSourceOrder)
	if err != nil {
		return "", nil, false, err
	}
	sources := orderedProfileSources(profile, now)
	if snap.AccountProfiles == nil {
		if resolver, ok := s.accounts.(FundingSelectionResolver); ok {
			pref, sources, err = resolver.FundingSelection(ctx, req.Principal.UserID)
			if err != nil {
				return "", nil, false, err
			}
		} else if resolver, ok := s.accounts.(FundingResolver); ok {
			sources, err = resolver.SubscriptionAccounts(ctx, req.Principal.UserID)
			if err != nil {
				return "", nil, false, err
			}
			if preferences, ok := s.accounts.(FundingPreferenceResolver); ok {
				pref, err = preferences.FundingPreference(ctx, req.Principal.UserID)
				if err != nil {
					return "", nil, false, err
				}
			}
		}
		pref, err = normalizeFundingPreference(pref, nil)
		if err != nil {
			return "", nil, false, err
		}
	}
	if pref == "subscription_only" && len(sources) == 0 {
		return "", nil, false, gateway.ErrInsufficientCredits
	}
	if pref == "wallet_only" {
		sources = nil
	}
	mode, err := freezeSourcePolicies(req, snap, prices, profile, now)
	return pref, sources, mode || pref != "subscription_first", err
}
