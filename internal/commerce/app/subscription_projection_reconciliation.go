package app

import (
	"fmt"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReconcileActiveSubscriptionLedgerProjections restores active subscription
// display projections from their authoritative ledger snapshot. It never
// credits, debits, settles, or releases quota. Subscriptions with an open
// reservation are skipped so an in-flight settlement cannot be double-counted.
func ReconcileActiveSubscriptionLedgerProjections(limit int) (int, error) {
	if limit <= 0 {
		limit = subscriptionMaintenanceBatchSize
	}

	var subscriptionIDs []int
	accountTable := (billingschema.BillingAccount{}).TableName()
	if err := platformdb.DB.Model(&commerceschema.UserSubscription{}).
		Select("user_subscriptions.id").
		Joins("JOIN "+accountTable+" AS billing_account ON billing_account.owner_type = ? AND billing_account.owner_id = user_subscriptions.id AND billing_account.account_type = ? AND billing_account.quota_unit = ?", "user_subscription", "subscription", "quota").
		Where("user_subscriptions.status = ? AND user_subscriptions.end_time > ?", "active", commercestore.GetDBTimestamp()).
		Order("user_subscriptions.id asc").
		Limit(limit).
		Scan(&subscriptionIDs).Error; err != nil {
		return 0, err
	}

	updated := 0
	for _, subscriptionID := range subscriptionIDs {
		changed, err := reconcileSubscriptionLedgerProjection(subscriptionID)
		if err != nil {
			return updated, err
		}
		if changed {
			updated++
		}
	}
	return updated, nil
}

func reconcileSubscriptionLedgerProjection(subscriptionID int) (bool, error) {
	if subscriptionID <= 0 {
		return false, fmt.Errorf("invalid user subscription id")
	}

	changed := false
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		sub := &commerceschema.UserSubscription{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ?", subscriptionID, "active").First(sub).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil
			}
			return err
		}

		var account billingschema.BillingAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_type = ? AND owner_type = ? AND owner_id = ? AND quota_unit = ?",
			"subscription", "user_subscription", sub.Id, "quota").First(&account).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil
			}
			return err
		}

		var entryCount int64
		if err := tx.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ?", account.AccountID).Count(&entryCount).Error; err != nil {
			return err
		}
		if entryCount == 0 {
			return nil
		}

		var snapshot billingschema.BillingBalanceSnapshot
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", account.AccountID).First(&snapshot).Error; err != nil {
			return err
		}
		if snapshot.ReservedBalance != 0 {
			return nil
		}

		targetUsed := sub.AmountTotal - snapshot.AvailableBalance
		if targetUsed < 0 {
			targetUsed = 0
		}
		if targetUsed > sub.AmountTotal {
			targetUsed = sub.AmountTotal
		}
		if targetUsed == sub.AmountUsed {
			return nil
		}

		previousUsed := sub.AmountUsed
		if err := tx.Model(&commerceschema.UserSubscription{}).Where("id = ? AND amount_used = ?", sub.Id, previousUsed).Update("amount_used", targetUsed).Error; err != nil {
			return err
		}
		_, err := billingdomain.RecordAdjustmentTx(tx, billingdomain.RecordAdjustmentParams{
			AccountID:      account.AccountID,
			IdempotencyKey: fmt.Sprintf("subscription-projection-reconcile:%d:%d:%d", sub.Id, previousUsed, targetUsed),
			ReasonCode:     "subscription_projection_reconciled",
			ReasonDetail:   "display projection synchronized to subscription ledger",
			ReferenceType:  "user_subscription",
			ReferenceID:    fmt.Sprintf("%d", sub.Id),
			OperatorType:   "subscription_maintenance",
			OperatorID:     "ledger_projection_reconcile",
		})
		if err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}
