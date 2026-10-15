package domain

const (
	PointsRulesOptionKey                     = "PointsRules"
	DefaultBonusQuotaPerPointUSD       int64 = 5
	DefaultMonthlyBonusConvertLimitUSD int64 = 500
)

type PointsRulesConfig struct {
	BonusQuotaPerPointUSD       int64            `json:"bonus_quota_per_point_usd"`
	MonthlyBonusConvertLimitUSD int64            `json:"monthly_bonus_convert_limit_usd"`
	PackagePurchasePoints       map[string]int64 `json:"package_purchase_points"`
}

func DefaultPointsRulesConfig() PointsRulesConfig {
	return PointsRulesConfig{
		BonusQuotaPerPointUSD:       DefaultBonusQuotaPerPointUSD,
		MonthlyBonusConvertLimitUSD: DefaultMonthlyBonusConvertLimitUSD,
		PackagePurchasePoints: map[string]int64{
			"Lite":     10,
			"Standard": 18,
			"Pro":      30,
			"Ultra":    60,
		},
	}
}
