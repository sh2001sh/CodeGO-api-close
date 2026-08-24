package app

import (
	"math"

	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

type usageConsumptionDiscount struct {
	QuotaBeforeDiscount int
	DiscountRate        float64
	DiscountMultiplier  float64
	NominalMultiplier   float64
	EffectiveMultiplier float64
	DiscountQuota       int
	QuotaAfterDiscount  int
	DiscountTitle       string
	PropID              int
	RemainingQuota      int64
	ChannelScope        string
}

func applyUsageConsumptionDiscount(relayInfo *relaycommon.RelayInfo, quota int) int {
	return calculateUsageConsumptionDiscount(relayInfo, quota).QuotaAfterDiscount
}

func calculateUsageConsumptionDiscount(relayInfo *relaycommon.RelayInfo, quota int) usageConsumptionDiscount {
	detail := calculateUsageConsumptionDiscountWithRate(quota, 0)
	if relayInfo == nil || relayInfo.ChannelMeta == nil || quota <= 0 {
		return detail
	}
	if relayInfo.BillingSource == BillingSourceSubscription && relayInfo.SubscriptionPackageMultiplier < 1 && relayInfo.SubscriptionPackageMultiplier > 0 {
		return detail
	}
	result, err := applyBlindBoxConsumptionDiscount(BlindBoxConsumptionDiscountRequest{
		RequestID: relayInfo.RequestId, UserID: relayInfo.UserId,
		ChannelID: relayInfo.ChannelId, ChannelScope: relayInfo.ChannelScope,
		ModelName: relayInfo.OriginModelName, UsingGroup: relayInfo.UsingGroup,
		Quota: quota,
	})
	if err != nil {
		platformobservability.SysError("apply blind box consumption discount: " + err.Error())
		return detail
	}
	detail.QuotaBeforeDiscount = result.QuotaBeforeDiscount
	detail.QuotaAfterDiscount = result.QuotaAfterDiscount
	detail.DiscountQuota = result.DiscountQuota
	detail.DiscountRate = result.DiscountRate
	detail.DiscountMultiplier = result.Multiplier
	detail.NominalMultiplier = result.NominalMultiplier
	detail.EffectiveMultiplier = result.EffectiveMultiplier
	detail.DiscountTitle = result.Title
	detail.PropID = result.PropID
	detail.RemainingQuota = result.RemainingDiscountQuota
	detail.ChannelScope = relayInfo.ChannelScope
	return detail
}

func calculateUsageConsumptionDiscountWithRate(quota int, rate float64) usageConsumptionDiscount {
	detail := usageConsumptionDiscount{
		QuotaBeforeDiscount: quota,
		DiscountMultiplier:  1,
		NominalMultiplier:   1,
		EffectiveMultiplier: 1,
		QuotaAfterDiscount:  quota,
	}
	if quota <= 0 || rate <= 0 {
		return detail
	}

	detail.DiscountRate = normalizeUsageMultiplier(rate)
	detail.DiscountMultiplier = normalizeUsageMultiplier(1 - rate)
	detail.NominalMultiplier = detail.DiscountMultiplier
	detail.QuotaAfterDiscount = int(math.Round(float64(quota) * detail.DiscountMultiplier))
	detail.DiscountQuota = quota - detail.QuotaAfterDiscount
	if quota > 0 {
		detail.EffectiveMultiplier = normalizeUsageMultiplier(float64(detail.QuotaAfterDiscount) / float64(quota))
	}
	detail.DiscountTitle = consumptionDiscountTitle(detail.DiscountMultiplier)
	return detail
}

func normalizeUsageMultiplier(value float64) float64 {
	return math.Round(value*10000) / 10000
}

func consumptionDiscountTitle(multiplier float64) string {
	switch {
	case math.Abs(multiplier-0.9) < 0.000001:
		return "0.9 倍率卡"
	case math.Abs(multiplier-0.95) < 0.000001:
		return "0.95 倍率卡"
	default:
		return "消费倍率卡"
	}
}

func appendUsageConsumptionDiscountInfo(
	other map[string]interface{},
	detail usageConsumptionDiscount,
) {
	if other == nil {
		return
	}
	other["quota_before_discount"] = detail.QuotaBeforeDiscount
	other["quota_after_discount"] = detail.QuotaAfterDiscount
	other["usage_discount_rate"] = detail.DiscountRate
	other["usage_discount_multiplier"] = detail.DiscountMultiplier
	other["usage_discount_nominal_multiplier"] = detail.NominalMultiplier
	other["usage_discount_effective_multiplier"] = detail.EffectiveMultiplier
	other["usage_discount_quota"] = detail.DiscountQuota
	if detail.DiscountRate > 0 {
		other["usage_discount_source"] = "blind_box_multiplier_card"
		other["usage_discount_title"] = detail.DiscountTitle
		other["usage_discount_prop_id"] = detail.PropID
		other["usage_discount_channel_scope"] = detail.ChannelScope
		other["usage_discount_remaining_quota"] = detail.RemainingQuota
	}
}
