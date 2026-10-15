package domain

import (
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"strings"
)

func NormalizeSubscriptionPlanType(planType string) string {
	switch strings.TrimSpace(strings.ToLower(planType)) {
	case commerceschema.SubscriptionPlanTypeStarter:
		return commerceschema.SubscriptionPlanTypeStarter
	case commerceschema.SubscriptionPlanTypeWeekly:
		return commerceschema.SubscriptionPlanTypeWeekly
	case commerceschema.SubscriptionPlanTypeDaily:
		return commerceschema.SubscriptionPlanTypeDaily
	default:
		return commerceschema.SubscriptionPlanTypeMonthly
	}
}

func NormalizeSubscriptionMembershipTier(tier string) string {
	switch strings.TrimSpace(strings.ToLower(tier)) {
	case commerceschema.SubscriptionMembershipTierLite:
		return commerceschema.SubscriptionMembershipTierLite
	case commerceschema.SubscriptionMembershipTierStandard:
		return commerceschema.SubscriptionMembershipTierStandard
	case commerceschema.SubscriptionMembershipTierPro:
		return commerceschema.SubscriptionMembershipTierPro
	case commerceschema.SubscriptionMembershipTierUltra:
		return commerceschema.SubscriptionMembershipTierUltra
	default:
		return commerceschema.SubscriptionMembershipTierNone
	}
}

func SubscriptionMembershipTierRank(tier string) int {
	switch NormalizeSubscriptionMembershipTier(tier) {
	case commerceschema.SubscriptionMembershipTierLite:
		return 1
	case commerceschema.SubscriptionMembershipTierStandard:
		return 2
	case commerceschema.SubscriptionMembershipTierPro:
		return 3
	case commerceschema.SubscriptionMembershipTierUltra:
		return 4
	default:
		return 0
	}
}

func NormalizeSubscriptionPurchaseType(purchaseType string) string {
	switch strings.TrimSpace(strings.ToLower(purchaseType)) {
	case commerceschema.SubscriptionPurchaseTypeGroupBuy:
		return commerceschema.SubscriptionPurchaseTypeGroupBuy
	case commerceschema.SubscriptionPurchaseTypeJoinGroup:
		return commerceschema.SubscriptionPurchaseTypeJoinGroup
	default:
		return commerceschema.SubscriptionPurchaseTypeNormal
	}
}

func NormalizeSubscriptionModelQuotaMap(input map[string]int64) commerceschema.SubscriptionModelQuotaMap {
	result := make(commerceschema.SubscriptionModelQuotaMap)
	for key, value := range input {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" || value <= 0 {
			continue
		}
		result[trimmedKey] = value
	}
	return result
}

func ParseSubscriptionModelQuotaMap(raw string) (commerceschema.SubscriptionModelQuotaMap, error) {
	if strings.TrimSpace(raw) == "" {
		return commerceschema.SubscriptionModelQuotaMap{}, nil
	}
	result := make(commerceschema.SubscriptionModelQuotaMap)
	if err := platformencoding.UnmarshalString(raw, &result); err != nil {
		return nil, err
	}
	return NormalizeSubscriptionModelQuotaMap(result), nil
}

func EncodeSubscriptionModelQuotaMap(input map[string]int64) (string, error) {
	normalized := NormalizeSubscriptionModelQuotaMap(input)
	if len(normalized) == 0 {
		return "", nil
	}
	raw, err := platformencoding.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func IsSubscriptionDayPassPlan(plan *commerceschema.SubscriptionPlan) bool {
	if plan == nil {
		return false
	}
	return plan.DurationUnit == commerceschema.SubscriptionDurationDay && plan.DurationValue > 0 && plan.DurationValue <= 2
}

func NormalizeResetPeriod(period string) string {
	switch strings.TrimSpace(period) {
	case commerceschema.SubscriptionResetDaily, commerceschema.SubscriptionResetWeekly, commerceschema.SubscriptionResetMonthly, commerceschema.SubscriptionResetCustom:
		return strings.TrimSpace(period)
	default:
		return commerceschema.SubscriptionResetNever
	}
}
