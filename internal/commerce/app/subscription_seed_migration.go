package app

import (
	"fmt"
	"math"
	"strings"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func getLegacyPresetPlanQuota(title string) (totalAmount int64, periodAmount int64, ok bool) {
	switch strings.TrimSpace(title) {
	case "Lite月卡":
		return quotaUnitsFromUSD(300), quotaUnitsFromUSD(75), true
	case "Standard月卡":
		return quotaUnitsFromUSD(600), quotaUnitsFromUSD(150), true
	case "Pro月卡":
		return quotaUnitsFromUSD(1500), quotaUnitsFromUSD(375), true
	case "Ultra月卡":
		return quotaUnitsFromUSD(4000), quotaUnitsFromUSD(1000), true
	case "50刀日卡":
		return quotaUnitsFromUSD(50), 0, true
	case "100刀日卡":
		return quotaUnitsFromUSD(100), 0, true
	default:
		return 0, 0, false
	}
}

func getLegacyPresetPlanQuotaUSD(title string) (totalAmount float64, periodAmount float64, ok bool) {
	switch strings.TrimSpace(title) {
	case "Lite月卡":
		return 300, 75, true
	case "Standard月卡":
		return 600, 150, true
	case "Pro月卡":
		return 1500, 375, true
	case "Ultra月卡":
		return 4000, 1000, true
	case "50刀日卡":
		return 50, 0, true
	case "100刀日卡":
		return 100, 0, true
	default:
		return 0, 0, false
	}
}

func isLegacyPresetPlainUSDValue(value int64, usd float64) bool {
	return value > 0 && usd > 0 && value == int64(math.Round(usd))
}

func isCollapsedPresetPeriodicQuota(plan *commerceschema.SubscriptionPlan, sub commerceschema.UserSubscription) bool {
	if plan == nil || commercedomain.NormalizeResetPeriod(plan.QuotaResetPeriod) == commerceschema.SubscriptionResetNever {
		return false
	}
	if plan.TotalAmount <= plan.PeriodAmount || sub.AmountTotal <= 0 || sub.PeriodAmount <= 0 {
		return false
	}
	return sub.AmountTotal == sub.PeriodAmount && sub.AmountTotal <= plan.PeriodAmount
}

func migratePresetUserSubscriptions(plan *commerceschema.SubscriptionPlan) error {
	if plan == nil || plan.Id <= 0 {
		return nil
	}
	_, _, ok := getLegacyPresetPlanQuota(plan.Title)
	if !ok {
		return nil
	}
	legacyTotalUSD, legacyPeriodUSD, _ := getLegacyPresetPlanQuotaUSD(plan.Title)

	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var subs []commerceschema.UserSubscription
		if err := tx.Where("plan_id = ?", plan.Id).Find(&subs).Error; err != nil {
			return err
		}
		for _, sub := range subs {
			updates := buildPresetSubscriptionMigration(sub, plan, legacyTotalUSD, legacyPeriodUSD)
			if len(updates) == 0 {
				continue
			}
			if err := reconcileMigratedSubscriptionLedgerTx(tx, sub, updates); err != nil {
				return err
			}
			updates["updated_at"] = platformruntime.GetTimestamp()
			if err := tx.Model(&commerceschema.UserSubscription{}).Where("id = ?", sub.Id).Updates(updates).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func buildPresetSubscriptionMigration(sub commerceschema.UserSubscription, plan *commerceschema.SubscriptionPlan, legacyTotalUSD float64, legacyPeriodUSD float64) map[string]any {
	updates := map[string]any{}
	activeMonthlyTopUp := sub.Status == "active" && sub.EndTime > platformruntime.GetTimestamp() && plan.TotalAmount > sub.AmountTotal
	fixTotal := isLegacyPresetPlainUSDValue(sub.AmountTotal, legacyTotalUSD) || isCollapsedPresetPeriodicQuota(plan, sub) || (plan.PeriodAmount == 0 && sub.PeriodAmount > 0 && sub.AmountTotal <= sub.PeriodAmount) || activeMonthlyTopUp
	// Preset updates only top up active users. Existing subscriptions with a
	// larger historical allocation must never be reduced by a plan revision.
	if fixTotal && plan.TotalAmount > sub.AmountTotal {
		updates["amount_total"] = plan.TotalAmount
	}
	if fixTotal && plan.TotalAmount > 0 && sub.AmountUsed > 0 {
		targetUsed := sub.AmountUsed
		if isLegacyPresetPlainUSDValue(sub.AmountTotal, legacyTotalUSD) {
			targetUsed = quotaUnitsFromUSD(float64(sub.AmountUsed))
		}
		if targetUsed > plan.TotalAmount {
			targetUsed = plan.TotalAmount
		}
		if targetUsed != sub.AmountUsed {
			updates["amount_used"] = targetUsed
		}
	}

	fixPeriod := isLegacyPresetPlainUSDValue(sub.PeriodAmount, legacyPeriodUSD) || isCollapsedPresetPeriodicQuota(plan, sub) || (plan.PeriodAmount == 0 && sub.PeriodAmount > 0)
	if fixPeriod && sub.PeriodAmount != plan.PeriodAmount {
		updates["period_amount"] = plan.PeriodAmount
	}
	if fixPeriod && plan.PeriodAmount > 0 && sub.PeriodUsed > 0 {
		targetUsed := sub.PeriodUsed
		if isLegacyPresetPlainUSDValue(sub.PeriodAmount, legacyPeriodUSD) {
			targetUsed = quotaUnitsFromUSD(float64(sub.PeriodUsed))
		}
		if targetUsed > plan.PeriodAmount {
			targetUsed = plan.PeriodAmount
		}
		if targetUsed != sub.PeriodUsed {
			updates["period_used"] = targetUsed
		}
	}
	if commercedomain.NormalizeResetPeriod(plan.QuotaResetPeriod) == commerceschema.SubscriptionResetNever || plan.PeriodAmount == 0 {
		if sub.PeriodUsed != 0 {
			updates["period_used"] = int64(0)
		}
		if sub.LastResetTime != 0 {
			updates["last_reset_time"] = int64(0)
		}
		if sub.NextResetTime != 0 {
			updates["next_reset_time"] = int64(0)
		}
	}
	return updates
}

func reconcileMigratedSubscriptionLedgerTx(tx *gorm.DB, sub commerceschema.UserSubscription, updates map[string]any) error {
	targetTotal, totalChanged := updates["amount_total"].(int64)
	if !totalChanged {
		targetTotal = sub.AmountTotal
	}
	targetUsed, usedChanged := updates["amount_used"].(int64)
	if !usedChanged {
		targetUsed = sub.AmountUsed
	}
	if !totalChanged && !usedChanged {
		return nil
	}
	targetAvailable := targetTotal - targetUsed
	if targetAvailable <= 0 {
		return nil
	}

	account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
		AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota",
	})
	if err != nil {
		return err
	}
	var snapshot billingschema.BillingBalanceSnapshot
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", account.AccountID).First(&snapshot).Error; err != nil {
		return err
	}
	if targetAvailable <= snapshot.AvailableBalance {
		return nil
	}
	delta := targetAvailable - snapshot.AvailableBalance
	_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
		AccountID:      account.AccountID,
		Amount:         delta,
		IdempotencyKey: fmt.Sprintf("subscription-migration:%d:%d:%d", sub.Id, targetTotal, targetUsed),
		ReasonCode:     "subscription_legacy_quota_migration",
		ReferenceType:  "user_subscription",
		ReferenceID:    fmt.Sprintf("%d", sub.Id),
		OperatorType:   "migration",
		OperatorID:     "preset_subscription_seed",
	})
	return err
}
