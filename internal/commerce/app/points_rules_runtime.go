package app

import (
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"strings"
)

func getPointsRulesConfig() commercedomain.PointsRulesConfig {
	cfg := commercedomain.DefaultPointsRulesConfig()
	platformconfig.OptionMapRWMutex.RLock()
	raw := platformconfig.OptionMap[commercedomain.PointsRulesOptionKey]
	platformconfig.OptionMapRWMutex.RUnlock()
	if strings.TrimSpace(raw) == "" {
		return cfg
	}

	var stored commercedomain.PointsRulesConfig
	if err := platformencoding.UnmarshalString(raw, &stored); err != nil {
		return cfg
	}
	return normalizePointsRulesConfig(stored)
}

func normalizePointsRulesConfig(input commercedomain.PointsRulesConfig) commercedomain.PointsRulesConfig {
	defaults := commercedomain.DefaultPointsRulesConfig()
	if input.BonusQuotaPerPointUSD <= 0 {
		input.BonusQuotaPerPointUSD = defaults.BonusQuotaPerPointUSD
	}
	if input.MonthlyBonusConvertLimitUSD <= 0 {
		input.MonthlyBonusConvertLimitUSD = defaults.MonthlyBonusConvertLimitUSD
	}
	if input.PackagePurchasePoints == nil {
		input.PackagePurchasePoints = defaults.PackagePurchasePoints
	}
	delete(input.PackagePurchasePoints, "DayPass50")
	delete(input.PackagePurchasePoints, "DayPass100")
	for key, value := range defaults.PackagePurchasePoints {
		if input.PackagePurchasePoints[key] <= 0 {
			input.PackagePurchasePoints[key] = value
		}
	}
	return input
}
