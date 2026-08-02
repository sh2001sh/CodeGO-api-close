package app

import (
	"errors"
	"time"

	"github.com/sh2001sh/new-api/constant"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func applyFirstPurchaseDiscountPreview(db *gorm.DB, userID int, plan *commerceschema.SubscriptionPlan, preview *commercedomain.SubscriptionPurchasePreview, now time.Time) error {
	if db == nil || preview == nil || preview.AmountDue <= 0 || !isFirstPurchaseDiscountPlan(plan) {
		return nil
	}
	setting := commercestore.GetPaymentSetting()
	if !isFirstPurchaseCampaignActive(setting, now.Unix()) {
		return nil
	}
	eligible, err := isFirstPurchaseDiscountEligible(db, userID)
	if err != nil || !eligible {
		return err
	}
	preview.FirstPurchaseDiscountApplied = true
	preview.FirstPurchaseDiscountMultiplier = setting.FirstPurchaseDiscountMultiplier
	preview.AmountDue = applyFirstPurchaseMultiplier(preview.AmountDue, setting.FirstPurchaseDiscountMultiplier)
	return nil
}

func applyFirstPurchaseDiscountTx(tx *gorm.DB, order *commerceschema.SubscriptionOrder, baseMoney float64, now time.Time) error {
	if tx == nil || order == nil {
		return errors.New("invalid first purchase discount order")
	}
	order.OriginalMoney = baseMoney
	order.Money = baseMoney
	setting := commercestore.GetPaymentSetting()
	if !isFirstPurchaseCampaignActive(setting, now.Unix()) || order.PurchaseType == commerceschema.SubscriptionPurchaseTypeFuel {
		return nil
	}
	plan, err := getSubscriptionPlanRecordTx(tx, order.PlanId)
	if err != nil {
		return err
	}
	if !isFirstPurchaseDiscountPlan(plan) {
		return nil
	}

	var user identityschema.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&user, order.UserId).Error; err != nil {
		return err
	}
	eligible, err := isFirstPurchaseDiscountEligible(tx, order.UserId)
	if err != nil || !eligible {
		return err
	}

	order.Money = applyFirstPurchaseMultiplier(baseMoney, setting.FirstPurchaseDiscountMultiplier)
	order.FirstPurchaseDiscountApplied = true
	order.FirstPurchaseDiscountMultiplier = setting.FirstPurchaseDiscountMultiplier
	order.FirstPurchaseDiscountStartAt = setting.FirstPurchaseDiscountStartAt
	order.FirstPurchaseDiscountEndAt = setting.FirstPurchaseDiscountEndAt
	return nil
}

func isFirstPurchaseDiscountEligible(db *gorm.DB, userID int) (bool, error) {
	if db == nil || userID <= 0 {
		return false, errors.New("invalid first purchase eligibility query")
	}
	var count int64
	eligiblePlanIDs := db.Model(&commerceschema.SubscriptionPlan{}).
		Select("id").
		Where("plan_type = ?", commerceschema.SubscriptionPlanTypeMonthly).
		Where("duration_unit = ?", commerceschema.SubscriptionDurationMonth)
	err := db.Model(&commerceschema.SubscriptionOrder{}).
		Where("user_id = ?", userID).
		Where("plan_id IN (?)", eligiblePlanIDs).
		Where("COALESCE(purchase_type, '') <> ?", commerceschema.SubscriptionPurchaseTypeFuel).
		Where("status = ? OR (status = ? AND first_purchase_discount_applied = ?)",
			constant.TopUpStatusSuccess,
			constant.TopUpStatusPending,
			true,
		).
		Count(&count).Error
	return count == 0, err
}

func isFirstPurchaseDiscountPlan(plan *commerceschema.SubscriptionPlan) bool {
	return plan != nil &&
		plan.PlanType == commerceschema.SubscriptionPlanTypeMonthly &&
		plan.DurationUnit == commerceschema.SubscriptionDurationMonth
}

func isFirstPurchaseCampaignActive(setting *commercestore.PaymentSetting, now int64) bool {
	if setting == nil || !setting.FirstPurchaseDiscountEnabled {
		return false
	}
	if setting.FirstPurchaseDiscountMultiplier <= 0 || setting.FirstPurchaseDiscountMultiplier >= 1 {
		return false
	}
	if setting.FirstPurchaseDiscountStartAt <= 0 || setting.FirstPurchaseDiscountEndAt <= setting.FirstPurchaseDiscountStartAt {
		return false
	}
	return now >= setting.FirstPurchaseDiscountStartAt && now <= setting.FirstPurchaseDiscountEndAt
}

func applyFirstPurchaseMultiplier(money float64, multiplier float64) float64 {
	return commercedomain.ApplyDiscountRateToMoney(money, 1-multiplier)
}
