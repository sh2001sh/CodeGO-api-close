package catalog

import "time"

// The separate activation flag is persisted in channel settings; early v3
// rows without that flag retain their original single-flag eligibility.
func CardUserEnabled(settings map[string]any, fallback bool) bool {
	if raw, exists := settings["multiplier_card_user_enabled"]; exists {
		value, _ := raw.(bool)
		return value
	}
	if market, ok := settings["market"].(map[string]any); ok {
		if raw, exists := market["multiplier_card_user_enabled"]; exists {
			value, _ := raw.(bool)
			return value
		}
	}
	return fallback
}

func (c MultiplierCard) IsConsumptionDiscount() bool {
	return (c.PropType == "" && c.MaxDiscountMicro == 0) || c.PropType == "consume_discount_95" || c.PropType == "consume_discount_90" || c.PropType == "consume_discount_10"
}

// PackageCard retains the separate zero-hour and monthly-pass funding policy.
// It never becomes a global wallet consumption discount.
func (p AccountProfile) PackageCard(kind string, now time.Time) (MultiplierCard, bool) {
	var chosen MultiplierCard
	found := false
	for _, card := range p.Cards {
		if card.PropType == kind && !card.ExpiresAt.IsZero() && now.Before(card.ExpiresAt) && (!found || card.MultiplierPPM < chosen.MultiplierPPM) {
			chosen, found = card, true
		}
	}
	return chosen, found
}

func (p AccountProfile) RequiresCardChannel(now time.Time) bool {
	for _, card := range p.Cards {
		if (card.IsConsumptionDiscount() || card.PropType == "monthly_pass_multiplier" || card.PropType == "zero_hour_multiplier") && !card.ExpiresAt.IsZero() && now.Before(card.ExpiresAt) {
			return true
		}
	}
	return false
}
