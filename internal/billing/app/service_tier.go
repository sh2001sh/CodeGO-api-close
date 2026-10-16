package app

import (
	"fmt"

	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
)

func appendServiceTierCalculation(other map[string]interface{}, info *relaycommon.RelayInfo, baseQuota, finalQuota int) {
	if info == nil || other == nil {
		return
	}
	multiplier := relaycommon.ServiceTierMultiplier(info.ServiceTier)
	other["service_tier"] = info.ServiceTier
	other["service_tier_multiplier"] = multiplier
	other["quota_before_service_tier"] = baseQuota
	other["quota_after_service_tier"] = info.ServiceTierQuota(baseQuota)
	discount := other["usage_discount_effective_multiplier"]
	if discount == nil {
		discount = 1
	}
	formula := fmt.Sprintf("round(%d × %d × %v) = %d", baseQuota, multiplier, discount, finalQuota)
	if info.BillingSource == BillingSourceSubscription && info.SubscriptionQuotaScale > 0 {
		formula += fmt.Sprintf("; round(%d × %g) = %d (subscription)", finalQuota, info.SubscriptionQuotaScale, BillingQuotaForLog(info, finalQuota))
	} else if charged := BillingQuotaForLog(info, finalQuota); charged != finalQuota {
		formula += fmt.Sprintf("; collected = %d", charged)
	}
	other["billing_calculation"] = formula
	other["billing_calculation_unit"] = "quota"
}
