package catalog

import (
	"encoding/json"
	"errors"
)

func readProfilePreference(profile *AccountProfile, raw []byte) error {
	var settings *struct {
		BillingPreference    string   `json:"billing_preference"`
		FundingSourceOrder   []string `json:"funding_source_order"`
		SubscriptionOrderIDs []int64  `json:"subscription_order_ids"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil || settings == nil {
		return errors.New("invalid funding preference settings")
	}
	switch settings.BillingPreference {
	case "", "subscription_first", "wallet_first", "subscription_only", "wallet_only":
	default:
		return errors.New("invalid billing preference")
	}
	order := settings.FundingSourceOrder
	if len(order) == 0 {
		switch settings.BillingPreference {
		case "", "subscription_first":
			order = []string{"subscription", "wallet"}
		case "wallet_first":
			order = []string{"wallet", "subscription"}
		case "subscription_only":
			order = []string{"subscription"}
		case "wallet_only":
			order = []string{"wallet"}
		}
	}
	if len(order) > 2 || len(settings.SubscriptionOrderIDs) > 1000 {
		return errors.New("invalid funding preference size")
	}
	seenSources := map[string]bool{}
	for _, source := range order {
		if (source != "subscription" && source != "wallet") || seenSources[source] {
			return errors.New("invalid funding source order")
		}
		seenSources[source] = true
	}
	seenSubscriptions := map[int64]bool{}
	for _, id := range settings.SubscriptionOrderIDs {
		if id <= 0 || seenSubscriptions[id] {
			return errors.New("invalid subscription order")
		}
		seenSubscriptions[id] = true
	}
	profile.BillingPreference = order[0] + "_only"
	if len(order) == 2 {
		profile.BillingPreference = order[0] + "_first"
	}
	profile.FundingSourceOrder = order
	profile.SubscriptionOrderIDs = settings.SubscriptionOrderIDs
	return nil
}
