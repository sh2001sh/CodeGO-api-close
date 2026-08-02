package app

import (
	"errors"
	"fmt"

	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"strings"
	"time"

	// HasStarterPurchaseWithin reports whether the user purchased a starter subscription within the window.
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

func HasStarterPurchaseWithin(userID int, window time.Duration) (bool, error) {
	if userID <= 0 {
		return false, nil
	}

	cutoff := platformruntime.GetTimestamp() - int64(window.Seconds())
	var count int64
	err := platformdb.DB.Model(&commerceschema.UserSubscription{}).
		Joins("JOIN subscription_plans ON subscription_plans.id = user_subscriptions.plan_id").
		Where("user_subscriptions.user_id = ? AND subscription_plans.plan_type = ? AND user_subscriptions.created_at >= ?", userID, commerceschema.SubscriptionPlanTypeStarter, cutoff).
		Count(&count).Error
	return count > 0, err
}

func hasStarterPurchaseWithinTx(tx *gorm.DB, userID int, window time.Duration) (bool, error) {
	if tx == nil || userID <= 0 {
		return false, nil
	}
	cutoff := platformruntime.GetTimestamp() - int64(window.Seconds())
	var count int64
	err := tx.Model(&commerceschema.UserSubscription{}).
		Joins("JOIN subscription_plans ON subscription_plans.id = user_subscriptions.plan_id").
		Where("user_subscriptions.user_id = ? AND subscription_plans.plan_type = ? AND user_subscriptions.created_at >= ?", userID, commerceschema.SubscriptionPlanTypeStarter, cutoff).
		Count(&count).Error
	return count > 0, err
}

func starterUpgradeBonusUSD(plan *commerceschema.SubscriptionPlan) float64 {
	if plan == nil {
		return 0
	}
	title := strings.ToLower(strings.TrimSpace(plan.Title))
	switch {
	case strings.Contains(title, "ultra"):
		return 100
	case strings.Contains(title, "pro"):
		return 60
	case strings.Contains(title, "standard"):
		return 30
	case strings.Contains(title, "lite"):
		return 10
	default:
		return 0
	}
}

func quotaUnitsToUSD(amount int64) float64 {
	if amount <= 0 || platformruntime.QuotaPerUnit <= 0 {
		return 0
	}
	return float64(amount) / platformruntime.QuotaPerUnit
}

func addSubscriptionBonusTx(tx *gorm.DB, sub *commerceschema.UserSubscription, bonusQuota int64) error {
	if tx == nil || sub == nil || bonusQuota <= 0 {
		return nil
	}
	sub.AmountTotal += bonusQuota
	if sub.PeriodAmount > 0 {
		sub.PeriodAmount += bonusQuota
	}
	if err := tx.Model(&commerceschema.UserSubscription{}).Where("id = ?", sub.Id).
		Updates(map[string]any{
			"amount_total":  sub.AmountTotal,
			"period_amount": sub.PeriodAmount,
			"updated_at":    platformruntime.GetTimestamp(),
		}).Error; err != nil {
		return err
	}
	return creditMaterializedSubscriptionBonusTx(tx, sub, bonusQuota)
}

// creditMaterializedSubscriptionBonusTx mirrors a subscription bonus into an
// existing ledger account. New subscriptions remain unmaterialized until first
// use, at which point their full updated quota is bootstrapped once.
func creditMaterializedSubscriptionBonusTx(tx *gorm.DB, sub *commerceschema.UserSubscription, bonusQuota int64) error {
	var account billingschema.BillingAccount
	err := tx.Where("account_type = ? AND owner_type = ? AND owner_id = ?", "subscription", "user_subscription", sub.Id).
		First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
		AccountID:      account.AccountID,
		Amount:         bonusQuota,
		IdempotencyKey: fmt.Sprintf("subscription-bonus:%d:%d:%d", sub.Id, sub.AmountTotal, bonusQuota),
		ReasonCode:     "subscription_bonus",
		ReferenceType:  "user_subscription",
		ReferenceID:    fmt.Sprintf("%d", sub.Id),
		OperatorType:   "commerce",
		OperatorID:     "subscription_bonus",
	})
	return err
}

// ApplySubscriptionPurchaseBonusTx applies the starter-to-monthly upgrade bonus.
func ApplySubscriptionPurchaseBonusTx(tx *gorm.DB, userID int, sub *commerceschema.UserSubscription, plan *commerceschema.SubscriptionPlan, preview *commercedomain.SubscriptionPurchasePreview) error {
	if tx == nil || sub == nil || plan == nil || preview == nil {
		return nil
	}
	planType := commercedomain.NormalizeSubscriptionPlanType(plan.PlanType)
	totalBonusUSD := 0.0

	if planType == commerceschema.SubscriptionPlanTypeMonthly {
		eligible, err := hasStarterPurchaseWithinTx(tx, userID, 72*time.Hour)
		if err != nil {
			return err
		}
		if eligible {
			totalBonusUSD += starterUpgradeBonusUSD(plan)
		}
	}

	if totalBonusUSD <= 0 {
		return nil
	}
	bonusQuota := quotaUnitsFromUSD(totalBonusUSD)
	if err := addSubscriptionBonusTx(tx, sub, bonusQuota); err != nil {
		return err
	}
	return auditapp.RecordLogTx(tx, userID, auditschema.LogTypeTopup, fmt.Sprintf("套餐升级奖励到账，套餐: %s，奖励额度: $%.2f", plan.Title, totalBonusUSD))
}
