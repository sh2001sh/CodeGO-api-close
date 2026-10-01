package catalog

import (
	"testing"
	"time"
)

func TestPackageCardsNeverBecomeConsumptionDiscounts(t *testing.T) {
	now := time.Unix(10000, 0)
	p := AccountProfile{Cards: []MultiplierCard{
		{ID: 1, PropType: "monthly_pass_multiplier", MultiplierPPM: 100000, ExpiresAt: now.Add(time.Minute)},
		{ID: 2, PropType: "zero_hour_multiplier", MultiplierPPM: 0, ExpiresAt: now.Add(time.Minute)},
		{ID: 3, PropType: "consume_discount_90", MultiplierPPM: 900000, ExpiresAt: now.Add(time.Hour), MaxDiscountMicro: 100, UsedDiscountMicro: 100},
	}}
	if p.CardMultiplier(now) != 0.9 {
		t.Fatal("package multiplier applied globally or legacy cap enforced")
	}
	if card, ok := p.PackageCard("monthly_pass_multiplier", now); !ok || card.MultiplierPPM != 100000 {
		t.Fatal("monthly entitlement missing")
	}
	if !p.RequiresCardChannel(now) || p.RequiresCardChannel(now.Add(time.Hour)) {
		t.Fatal("card routing policy expiry mismatch")
	}
	if _, ok := p.PackageCard("zero_hour_multiplier", now.Add(time.Minute)); ok {
		t.Fatal("expired zero-hour entitlement retained")
	}
}

func TestNativeUncappedCardIsAConsumptionBenefit(t *testing.T) {
	now := time.Unix(10000, 0)
	card := MultiplierCard{ID: 19, MultiplierPPM: 750000, ExpiresAt: now.Add(time.Minute)}
	p := AccountProfile{Cards: []MultiplierCard{card}}
	if !card.IsConsumptionDiscount() || p.CardMultiplier(now) != 0.75 || !p.RequiresCardChannel(now) {
		t.Fatal("activated native reward was excluded from consumption or eligible-channel routing")
	}
	if p.CardMultiplier(card.ExpiresAt) != 1 || p.RequiresCardChannel(card.ExpiresAt) {
		t.Fatal("native card remained usable at exact expiry")
	}
}

func TestCardActivationFlagFailsClosedAndRetainsSeparateFlags(t *testing.T) {
	for _, settings := range []map[string]any{
		{"multiplier_card_user_enabled": false},
		{"multiplier_card_user_enabled": "true"},
		{"market": map[string]any{"multiplier_card_user_enabled": false}},
		{"market": map[string]any{"multiplier_card_user_enabled": 1}},
	} {
		if CardUserEnabled(settings, true) {
			t.Fatal("explicit disabled or malformed activation was replaced by support flag")
		}
	}
	if !CardUserEnabled(nil, true) || !CardUserEnabled(map[string]any{"multiplier_card_user_enabled": true}, false) {
		t.Fatal("compatibility fallback or explicit activation was lost")
	}
}
