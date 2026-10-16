package app

import (
	"errors"
	"fmt"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

// EnsureDefaultSubscriptionPlans retires legacy sales without changing purchased snapshots.
func EnsureDefaultSubscriptionPlans() error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if platformdb.UsingPostgreSQL {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(71020261015)).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&commerceschema.SubscriptionPlan{}).
			Where("plan_type <> ? AND internal_only = ?", commerceschema.SubscriptionPlanTypeCreditPack, false).
			Updates(map[string]any{"enabled": false, "lucky_draw_enabled": false, "group_buy_enabled": false}).Error; err != nil {
			return err
		}
		for _, plan := range defaultSubscriptionPlans() {
			var existing commerceschema.SubscriptionPlan
			err := tx.Where("title = ?", plan.Title).First(&existing).Error
			if err == nil {
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := tx.Create(&plan).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func defaultSubscriptionPlans() []commerceschema.SubscriptionPlan {
	tiers := []struct{ price, credits float64 }{{64, 67.2}, {128, 135.68}, {500, 535}, {1000, 1080}, {3000, 3300}}
	plans := make([]commerceschema.SubscriptionPlan, 0, len(tiers))
	for i, tier := range tiers {
		plans = append(plans, commerceschema.SubscriptionPlan{
			Title: fmt.Sprintf("%g元永久额度包", tier.price), Currency: "CNY", PriceAmount: tier.price,
			DurationUnit: commerceschema.SubscriptionDurationPermanent, DurationValue: 1,
			Enabled: true, SortOrder: 100 - i, TotalAmount: quotaUnitsFromUSD(tier.credits),
			PlanType:         commerceschema.SubscriptionPlanTypeCreditPack,
			QuotaResetPeriod: commerceschema.SubscriptionResetNever,
		})
	}
	return plans
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
