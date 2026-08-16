package app

import (
	"math"
	"testing"

	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
)

func TestCalculateUsageConsumptionDiscountWithRate(t *testing.T) {
	detail := calculateUsageConsumptionDiscountWithRate(1001, 0.10)

	if detail.QuotaBeforeDiscount != 1001 {
		t.Fatalf("unexpected quota before discount: %d", detail.QuotaBeforeDiscount)
	}
	if math.Abs(detail.DiscountMultiplier-0.9) > 0.000001 {
		t.Fatalf("unexpected discount multiplier: %f", detail.DiscountMultiplier)
	}
	if detail.QuotaAfterDiscount != 901 {
		t.Fatalf("expected rounded quota 901, got %d", detail.QuotaAfterDiscount)
	}
	if detail.DiscountQuota != 100 {
		t.Fatalf("unexpected discount quota: %d", detail.DiscountQuota)
	}
	if detail.DiscountTitle != "0.9 倍率卡" {
		t.Fatalf("unexpected discount title: %s", detail.DiscountTitle)
	}
}

func TestAppendUsageConsumptionDiscountInfo(t *testing.T) {
	other := map[string]interface{}{}
	detail := calculateUsageConsumptionDiscountWithRate(2000, 0.05)

	appendUsageConsumptionDiscountInfo(other, detail)

	if other["quota_before_discount"] != 2000 {
		t.Fatalf("unexpected quota before discount: %#v", other)
	}
	if other["quota_after_discount"] != 1900 {
		t.Fatalf("unexpected quota after discount: %#v", other)
	}
	if other["usage_discount_title"] != "0.95 倍率卡" {
		t.Fatalf("unexpected discount title: %#v", other)
	}
	if other["usage_discount_source"] != "blind_box_multiplier_card" {
		t.Fatalf("unexpected discount source: %#v", other)
	}
}

func TestNoUsageConsumptionDiscountKeepsQuota(t *testing.T) {
	detail := calculateUsageConsumptionDiscountWithRate(888, 0)

	if detail.QuotaAfterDiscount != 888 || detail.DiscountMultiplier != 1 {
		t.Fatalf("unexpected no-discount detail: %#v", detail)
	}
	if detail.DiscountTitle != "" {
		t.Fatalf("no-discount detail should not have a title: %#v", detail)
	}
}

func TestPackageMultiplierDoesNotStackBlindBoxConsumptionDiscount(t *testing.T) {
	previousHooks := subscriptionFundingHooks
	discountCalled := false
	RegisterSubscriptionFundingHooks(SubscriptionFundingHooks{
		GetMonthlyPassMultiplier: func(int) float64 { return 0.1 },
		ApplyBlindBoxConsumptionDiscount: func(request BlindBoxConsumptionDiscountRequest) (BlindBoxConsumptionDiscountResult, error) {
			discountCalled = true
			return BlindBoxConsumptionDiscountResult{QuotaBeforeDiscount: request.Quota, QuotaAfterDiscount: request.Quota / 10}, nil
		},
	})
	t.Cleanup(func() { RegisterSubscriptionFundingHooks(previousHooks) })

	detail := calculateUsageConsumptionDiscount(&relaycommon.RelayInfo{
		UserId: 42, BillingSource: BillingSourceSubscription, SubscriptionPackageMultiplier: 0.1,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, 1_000)

	if discountCalled {
		t.Fatal("blind-box multiplier must not stack on package-funded requests")
	}
	if detail.QuotaAfterDiscount != 1_000 {
		t.Fatalf("unexpected stacked discount: %#v", detail)
	}
}
