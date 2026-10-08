package ledger

import (
	"encoding/json"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func conversionFundingMetadata(paid, reward int64) map[string]any {
	return map[string]any{
		"source": "subscription_conversion", "subscription_id": int64(13), "original_order_id": int64(27),
		"paid_principal_credits": paid, "reward_credits": reward,
		"revenue_multiplier_ppm": int64(800000), "non_transferable": reward > 0, "non_refundable": reward > 0,
	}
}

func TestFundingV2ConversionRejectsMissingOrInexactAttribution(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"missing_revenue", func(m map[string]any) { delete(m, "revenue_multiplier_ppm") }},
		{"missing_order", func(m map[string]any) { delete(m, "original_order_id") }},
		{"nonpaid_order", func(m map[string]any) { m["original_order_id"] = int64(0) }},
		{"floating_principal", func(m map[string]any) { m["paid_principal_credits"] = float64(80) }},
		{"mixed_slices", func(m map[string]any) { m["paid_principal_credits"], m["reward_credits"] = int64(70), int64(10) }},
		{"negative_reward", func(m map[string]any) { m["reward_credits"] = int64(-1) }},
		{"wrong_amount", func(m map[string]any) { m["paid_principal_credits"] = int64(81) }},
		{"overflow", func(m map[string]any) { m["paid_principal_credits"] = json.Number("9223372036854775808") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta := conversionFundingMetadata(80, 0)
			tc.edit(meta)
			if _, _, _, err := fundingEntryPolicy(billing.Entry{Amount: 80, Metadata: meta}); err == nil {
				t.Fatal("invalid conversion funding was accepted")
			}
		})
	}
	meta := conversionFundingMetadata(0, 20)
	delete(meta, "non_refundable")
	if _, _, _, err := fundingEntryPolicy(billing.Entry{Amount: 20, Metadata: meta}); err == nil {
		t.Fatal("reward conversion became refundable")
	}
}

func TestFundingV2ExactPrincipalAndRewardPolicies(t *testing.T) {
	const exact = int64(9007199254740993)
	meta := conversionFundingMetadata(exact, 0)
	meta["paid_principal_credits"] = json.Number("9007199254740993")
	ppm, transfer, refund, err := fundingEntryPolicy(billing.Entry{Amount: credits.Micro(exact), Metadata: meta})
	if err != nil || ppm == nil || *ppm != 800000 || transfer || refund {
		t.Fatalf("paid policy=%v/%t/%t err=%v", ppm, transfer, refund, err)
	}
	meta = conversionFundingMetadata(0, 20)
	meta["original_order_id"] = int64(0)
	ppm, transfer, refund, err = fundingEntryPolicy(billing.Entry{Amount: 20, Metadata: meta})
	if err != nil || ppm == nil || *ppm != 0 || !transfer || !refund {
		t.Fatalf("reward policy=%v/%t/%t err=%v", ppm, transfer, refund, err)
	}
	ppm, transfer, refund, err = fundingEntryPolicy(billing.Entry{Amount: 20, Kind: "reward", Metadata: map[string]any{"source": "referral_reward", "revenue_multiplier_ppm": 999999}})
	if err != nil || ppm == nil || *ppm != 0 || !transfer || !refund {
		t.Fatalf("referral policy=%v/%t/%t err=%v", ppm, transfer, refund, err)
	}
}
