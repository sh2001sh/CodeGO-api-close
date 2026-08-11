package app

import (
	"errors"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

// EnsureDefaultSubscriptionPlans creates built-in plans and repairs legacy preset snapshots.
func EnsureDefaultSubscriptionPlans() error {
	plans := defaultSubscriptionPlans()
	for index := range plans {
		existing, err := findPresetSubscriptionPlan(plans[index].Title)
		if err == nil {
			updates := syncPresetSubscriptionPlanFields(existing, plans[index])
			if existing.Title != plans[index].Title {
				updates["title"] = plans[index].Title
			}
			if len(updates) > 0 {
				if err := platformdb.DB.Model(existing).Updates(updates).Error; err != nil {
					return err
				}
				InvalidateSubscriptionPlanCache(existing.Id)
			}
			migrationPlan := plans[index]
			migrationPlan.Id = existing.Id
			if err := migratePresetUserSubscriptions(&migrationPlan); err != nil {
				return err
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := platformdb.DB.Create(&plans[index]).Error; err != nil {
			return err
		}
		if err := migratePresetUserSubscriptions(&plans[index]); err != nil {
			return err
		}
	}
	return nil
}

func defaultSubscriptionPlans() []commerceschema.SubscriptionPlan {
	return []commerceschema.SubscriptionPlan{
		{
			Title:                "Lite月卡",
			Subtitle:             "月卡",
			PriceAmount:          49,
			Currency:             "CNY",
			DurationUnit:         commerceschema.SubscriptionDurationMonth,
			DurationValue:        1,
			Enabled:              true,
			InternalOnly:         false,
			SortOrder:            70,
			TotalAmount:          quotaUnitsFromUSD(350),
			PeriodAmount:         0,
			QuotaResetPeriod:     commerceschema.SubscriptionResetNever,
			ModelLimits:          "",
			UpgradeGroup:         "",
			MaxPurchasePerUser:   0,
			PlanType:             commerceschema.SubscriptionPlanTypeMonthly,
			MembershipTier:       commerceschema.SubscriptionMembershipTierLite,
			LuckyDrawEnabled:     true,
			BlindBoxBenefitCount: 0,
			GroupBuyEnabled:      true,
			GroupBuyBonus2:       25,
			GroupBuyBonus3:       45,
			GroupBuyBonus5:       65,
			FuelEnabled:          true,
			FuelUnitPrice:        0.175,
			FuelMinQuota:         quotaUnitsFromUSD(10),
			FuelQuotaStep:        quotaUnitsFromUSD(10),
		},
		{
			Title:                "Standard月卡",
			Subtitle:             "月卡",
			PriceAmount:          89,
			Currency:             "CNY",
			DurationUnit:         commerceschema.SubscriptionDurationMonth,
			DurationValue:        1,
			Enabled:              true,
			InternalOnly:         false,
			SortOrder:            80,
			TotalAmount:          quotaUnitsFromUSD(650),
			PeriodAmount:         0,
			QuotaResetPeriod:     commerceschema.SubscriptionResetNever,
			PlanType:             commerceschema.SubscriptionPlanTypeMonthly,
			MembershipTier:       commerceschema.SubscriptionMembershipTierStandard,
			LuckyDrawEnabled:     true,
			BlindBoxBenefitCount: 0,
			GroupBuyEnabled:      true,
			GroupBuyBonus2:       50,
			GroupBuyBonus3:       90,
			GroupBuyBonus5:       115,
			FuelEnabled:          true,
			FuelUnitPrice:        0.170,
			FuelMinQuota:         quotaUnitsFromUSD(10),
			FuelQuotaStep:        quotaUnitsFromUSD(10),
		},
		{
			Title:                "Pro月卡",
			Subtitle:             "月卡",
			PriceAmount:          169,
			Currency:             "CNY",
			DurationUnit:         commerceschema.SubscriptionDurationMonth,
			DurationValue:        1,
			Enabled:              true,
			InternalOnly:         false,
			SortOrder:            60,
			TotalAmount:          quotaUnitsFromUSD(1260),
			PeriodAmount:         0,
			QuotaResetPeriod:     commerceschema.SubscriptionResetNever,
			PlanType:             commerceschema.SubscriptionPlanTypeMonthly,
			MembershipTier:       commerceschema.SubscriptionMembershipTierPro,
			LuckyDrawEnabled:     true,
			BlindBoxBenefitCount: 0,
			GroupBuyEnabled:      true,
			GroupBuyBonus2:       95,
			GroupBuyBonus3:       170,
			GroupBuyBonus5:       240,
			FuelEnabled:          true,
			FuelUnitPrice:        0.160,
			FuelMinQuota:         quotaUnitsFromUSD(10),
			FuelQuotaStep:        quotaUnitsFromUSD(10),
		},
		{
			Title:                "Ultra月卡",
			Subtitle:             "月卡",
			PriceAmount:          299,
			Currency:             "CNY",
			DurationUnit:         commerceschema.SubscriptionDurationMonth,
			DurationValue:        1,
			Enabled:              true,
			InternalOnly:         false,
			SortOrder:            50,
			TotalAmount:          quotaUnitsFromUSD(2300),
			PeriodAmount:         0,
			QuotaResetPeriod:     commerceschema.SubscriptionResetNever,
			PlanType:             commerceschema.SubscriptionPlanTypeMonthly,
			MembershipTier:       commerceschema.SubscriptionMembershipTierUltra,
			LuckyDrawEnabled:     true,
			BlindBoxBenefitCount: 0,
			GroupBuyEnabled:      true,
			GroupBuyBonus2:       170,
			GroupBuyBonus3:       300,
			GroupBuyBonus5:       420,
			FuelEnabled:          true,
			FuelUnitPrice:        0.150,
			FuelMinQuota:         quotaUnitsFromUSD(10),
			FuelQuotaStep:        quotaUnitsFromUSD(10),
		},
		{
			Title:              "新人体验卡",
			Subtitle:           "新人专区",
			PriceAmount:        1.9,
			Currency:           "CNY",
			DurationUnit:       commerceschema.SubscriptionDurationDay,
			DurationValue:      1,
			Enabled:            true,
			InternalOnly:       false,
			SortOrder:          90,
			TotalAmount:        quotaUnitsFromUSD(15),
			QuotaResetPeriod:   commerceschema.SubscriptionResetNever,
			MaxPurchasePerUser: 1,
			PlanType:           commerceschema.SubscriptionPlanTypeStarter,
			GroupBuyEnabled:    false,
		},
		{
			Title:            "标准周卡",
			Subtitle:         "周卡",
			PriceAmount:      34.9,
			Currency:         "CNY",
			DurationUnit:     commerceschema.SubscriptionDurationDay,
			DurationValue:    7,
			Enabled:          true,
			InternalOnly:     false,
			SortOrder:        40,
			TotalAmount:      quotaUnitsFromUSD(220),
			QuotaResetPeriod: commerceschema.SubscriptionResetNever,
			PlanType:         commerceschema.SubscriptionPlanTypeWeekly,
			GroupBuyEnabled:  true,
			GroupBuyBonus2:   20,
			GroupBuyBonus3:   35,
			GroupBuyBonus5:   55,
		},
		{
			Title:            "50刀日卡",
			Subtitle:         "日卡",
			PriceAmount:      8.9,
			Currency:         "CNY",
			DurationUnit:     commerceschema.SubscriptionDurationDay,
			DurationValue:    1,
			Enabled:          true,
			InternalOnly:     false,
			SortOrder:        30,
			TotalAmount:      quotaUnitsFromUSD(50),
			QuotaResetPeriod: commerceschema.SubscriptionResetNever,
			PlanType:         commerceschema.SubscriptionPlanTypeDaily,
			GroupBuyEnabled:  false,
		},
		{
			Title:            "100刀日卡",
			Subtitle:         "日卡",
			PriceAmount:      16.9,
			Currency:         "CNY",
			DurationUnit:     commerceschema.SubscriptionDurationDay,
			DurationValue:    1,
			Enabled:          true,
			InternalOnly:     false,
			SortOrder:        20,
			TotalAmount:      quotaUnitsFromUSD(100),
			QuotaResetPeriod: commerceschema.SubscriptionResetNever,
			PlanType:         commerceschema.SubscriptionPlanTypeDaily,
			GroupBuyEnabled:  false,
		},
	}
}

func legacyPresetSubscriptionPlanTitleMap() map[string]string {
	return map[string]string{
		"Lite":         "Lite月卡",
		"Standard":     "Standard月卡",
		"Pro":          "Pro月卡",
		"Ultra":        "Ultra月卡",
		"Day Pass 50":  "50刀日卡",
		"Day Pass 100": "100刀日卡",
	}
}

func getPresetPlanLegacyTitles(title string) []string {
	titleMap := legacyPresetSubscriptionPlanTitleMap()
	aliases := make([]string, 0, 2)
	for legacy, current := range titleMap {
		if current == title {
			aliases = append(aliases, legacy)
		}
	}
	return aliases
}

func findPresetSubscriptionPlan(title string) (*commerceschema.SubscriptionPlan, error) {
	var existing commerceschema.SubscriptionPlan
	err := platformdb.DB.Where("title = ?", title).First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	for _, legacyTitle := range getPresetPlanLegacyTitles(title) {
		err = platformdb.DB.Where("title = ?", legacyTitle).First(&existing).Error
		if err == nil {
			return &existing, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func syncPresetSubscriptionPlanFields(existing *commerceschema.SubscriptionPlan, preset commerceschema.SubscriptionPlan) map[string]interface{} {
	if existing == nil {
		return nil
	}
	updates := make(map[string]interface{})
	if existing.Subtitle != preset.Subtitle {
		updates["subtitle"] = preset.Subtitle
	}
	if existing.PriceAmount != preset.PriceAmount {
		updates["price_amount"] = preset.PriceAmount
	}
	if existing.Currency != preset.Currency {
		updates["currency"] = preset.Currency
	}
	if existing.DurationUnit != preset.DurationUnit {
		updates["duration_unit"] = preset.DurationUnit
	}
	if existing.DurationValue != preset.DurationValue {
		updates["duration_value"] = preset.DurationValue
	}
	if existing.CustomSeconds != preset.CustomSeconds {
		updates["custom_seconds"] = preset.CustomSeconds
	}
	if existing.Enabled != preset.Enabled {
		updates["enabled"] = preset.Enabled
	}
	if existing.InternalOnly != preset.InternalOnly {
		updates["internal_only"] = preset.InternalOnly
	}
	if existing.SortOrder != preset.SortOrder {
		updates["sort_order"] = preset.SortOrder
	}
	if existing.MaxPurchasePerUser != preset.MaxPurchasePerUser {
		updates["max_purchase_per_user"] = preset.MaxPurchasePerUser
	}
	if commercedomain.NormalizeSubscriptionPlanType(existing.PlanType) != commercedomain.NormalizeSubscriptionPlanType(preset.PlanType) {
		updates["plan_type"] = commercedomain.NormalizeSubscriptionPlanType(preset.PlanType)
	}
	if commercedomain.NormalizeSubscriptionMembershipTier(existing.MembershipTier) != commercedomain.NormalizeSubscriptionMembershipTier(preset.MembershipTier) {
		updates["membership_tier"] = commercedomain.NormalizeSubscriptionMembershipTier(preset.MembershipTier)
	}
	if existing.LuckyDrawEnabled != preset.LuckyDrawEnabled {
		updates["lucky_draw_enabled"] = preset.LuckyDrawEnabled
	}
	if existing.BlindBoxBenefitCount != preset.BlindBoxBenefitCount {
		updates["blind_box_benefit_count"] = preset.BlindBoxBenefitCount
	}
	if existing.GroupBuyEnabled != preset.GroupBuyEnabled {
		updates["group_buy_enabled"] = preset.GroupBuyEnabled
	}
	if existing.GroupBuyBonus2 != preset.GroupBuyBonus2 {
		updates["group_buy_bonus2"] = preset.GroupBuyBonus2
	}
	if existing.GroupBuyBonus3 != preset.GroupBuyBonus3 {
		updates["group_buy_bonus3"] = preset.GroupBuyBonus3
	}
	if existing.GroupBuyBonus5 != preset.GroupBuyBonus5 {
		updates["group_buy_bonus5"] = preset.GroupBuyBonus5
	}
	if existing.FuelEnabled != preset.FuelEnabled {
		updates["fuel_enabled"] = preset.FuelEnabled
	}
	if existing.FuelUnitPrice != preset.FuelUnitPrice {
		updates["fuel_unit_price"] = preset.FuelUnitPrice
	}
	if existing.FuelMinQuota != preset.FuelMinQuota {
		updates["fuel_min_quota"] = preset.FuelMinQuota
	}
	if existing.FuelQuotaStep != preset.FuelQuotaStep {
		updates["fuel_quota_step"] = preset.FuelQuotaStep
	}
	if existing.UpgradeGroup != preset.UpgradeGroup {
		updates["upgrade_group"] = preset.UpgradeGroup
	}
	if existing.TotalAmount != preset.TotalAmount {
		updates["total_amount"] = preset.TotalAmount
	}
	if existing.PeriodAmount != preset.PeriodAmount {
		updates["period_amount"] = preset.PeriodAmount
	}
	if existing.ModelLimits != preset.ModelLimits {
		updates["model_limits"] = preset.ModelLimits
	}
	if existing.QuotaResetPeriod != preset.QuotaResetPeriod {
		updates["quota_reset_period"] = preset.QuotaResetPeriod
	}
	if existing.QuotaResetCustomSeconds != preset.QuotaResetCustomSeconds {
		updates["quota_reset_custom_seconds"] = preset.QuotaResetCustomSeconds
	}
	return updates
}
