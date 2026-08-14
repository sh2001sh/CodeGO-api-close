package app

import "math"

type usageConsumptionDiscount struct {
	QuotaBeforeDiscount int
	DiscountRate        float64
	DiscountMultiplier  float64
	DiscountQuota       int
	QuotaAfterDiscount  int
	DiscountTitle       string
}

func getEffectiveConsumptionDiscountRate(userID int) float64 {
	return getBlindBoxConsumptionDiscountRate(userID)
}

func applyUsageConsumptionDiscount(userID int, quota int) int {
	return calculateUsageConsumptionDiscount(userID, quota).QuotaAfterDiscount
}

func calculateUsageConsumptionDiscount(userID int, quota int) usageConsumptionDiscount {
	return calculateUsageConsumptionDiscountWithRate(
		quota,
		getEffectiveConsumptionDiscountRate(userID),
	)
}

func calculateUsageConsumptionDiscountWithRate(quota int, rate float64) usageConsumptionDiscount {
	detail := usageConsumptionDiscount{
		QuotaBeforeDiscount: quota,
		DiscountMultiplier:  1,
		QuotaAfterDiscount:  quota,
	}
	if quota <= 0 || rate <= 0 {
		return detail
	}

	detail.DiscountRate = rate
	detail.DiscountMultiplier = 1 - rate
	detail.QuotaAfterDiscount = int(math.Round(float64(quota) * detail.DiscountMultiplier))
	detail.DiscountQuota = quota - detail.QuotaAfterDiscount
	detail.DiscountTitle = consumptionDiscountTitle(detail.DiscountMultiplier)
	return detail
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
	other["usage_discount_quota"] = detail.DiscountQuota
	if detail.DiscountRate > 0 {
		other["usage_discount_source"] = "blind_box_multiplier_card"
		other["usage_discount_title"] = detail.DiscountTitle
	}
}
