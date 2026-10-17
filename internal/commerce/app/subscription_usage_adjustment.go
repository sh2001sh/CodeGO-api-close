package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AdjustUserSubscriptionUsageDelta commits post-settlement task adjustments to
// the authoritative ledger and its usage projection together. The operation key
// belongs to the originating task/request and is preserved across retries.
func AdjustUserSubscriptionUsageDelta(subscriptionID int, modelName string, delta int64, operationKey string) (bool, error) {
	var applied bool
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		applied, err = AdjustUserSubscriptionUsageDeltaTx(tx, subscriptionID, modelName, delta, operationKey)
		return err
	})
	return applied && err == nil, err
}

func AdjustUserSubscriptionUsageDeltaTx(tx *gorm.DB, subscriptionID int, modelName string, delta int64, operationKey string) (bool, error) {
	if tx == nil || subscriptionID <= 0 || strings.TrimSpace(operationKey) == "" || delta == math.MinInt64 {
		return false, errors.New("valid transaction, subscription and adjustment identity are required")
	}
	if delta == 0 {
		return false, nil
	}
	sub, err := lockUserSubscriptionRecordTx(tx, subscriptionID)
	if err != nil {
		return false, err
	}
	account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
		AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(subscriptionID), QuotaUnit: "quota",
	})
	if err != nil {
		return false, err
	}
	key := "subscription-usage-adjust:" + operationKey
	var prior billingschema.BillingLedgerEntry
	query := tx.Where("idempotency_key = ?", key).Limit(1).Find(&prior)
	if query.Error != nil {
		return false, query.Error
	}
	if query.RowsAffected > 0 {
		var evidence struct {
			Delta int64  `json:"delta"`
			Model string `json:"model"`
		}
		if json.Unmarshal(prior.Metadata, &evidence) != nil || prior.AccountID != account.AccountID || evidence.Delta != delta || evidence.Model != modelName {
			return false, billingdomain.ErrLedgerConflict
		}
		return false, nil
	}
	plan, err := getSubscriptionPlanRecordTx(tx, sub.PlanId)
	if err != nil {
		return false, err
	}
	plan = creditPackSnapshotPlan(plan, sub)
	if sub.AmountTotal == 0 {
		// Unlimited subscriptions track usage without a finite spendable balance.
		// A zero-balance receipt still makes the usage adjustment replayable.
		if sub.AmountUsed < 0 || (delta > 0 && sub.AmountUsed > math.MaxInt64-delta) || (delta < 0 && -delta > sub.AmountUsed) {
			return false, errors.New("unlimited subscription usage adjustment is out of range")
		}
		if err := applySubscriptionUsageDelta(plan, sub, modelName, delta); err != nil {
			return false, err
		}
		entry, err := billingdomain.RecordAdjustmentTx(tx, billingdomain.RecordAdjustmentParams{
			AccountID: account.AccountID, IdempotencyKey: key, ReasonCode: "subscription_unlimited_usage_adjustment",
			ReferenceType: "user_subscription", ReferenceID: fmt.Sprint(sub.Id),
		})
		if err != nil {
			return false, err
		}
		metadata, err := json.Marshal(map[string]any{"delta": delta, "model": modelName})
		if err != nil {
			return false, err
		}
		if err := tx.Model(entry).Update("metadata", metadata).Error; err != nil {
			return false, err
		}
		if err := tx.Save(sub).Error; err != nil {
			return false, err
		}
		return true, nil
	}
	backed, err := subscriptionLedgerHasEntriesTx(tx, account.AccountID)
	if err != nil {
		return false, err
	}
	if !backed {
		if sub.AmountTotal <= 0 || sub.AmountUsed < 0 || sub.AmountUsed > sub.AmountTotal {
			return false, errors.New("subscription balance is invalid for a ledger adjustment")
		}
		available := sub.AmountTotal - sub.AmountUsed
		bootstrap := fmt.Sprintf("subscription-bootstrap:%d", sub.Id)
		if available > 0 {
			_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
				AccountID: account.AccountID, Amount: available, IdempotencyKey: bootstrap,
				ReasonCode: "subscription_balance_bootstrap", ReferenceType: "user_subscription", ReferenceID: fmt.Sprint(sub.Id),
			})
		} else {
			_, err = billingdomain.RecordAdjustmentTx(tx, billingdomain.RecordAdjustmentParams{
				AccountID: account.AccountID, IdempotencyKey: bootstrap, ReasonCode: "subscription_balance_bootstrap",
				ReferenceType: "user_subscription", ReferenceID: fmt.Sprint(sub.Id),
			})
		}
		if err != nil {
			return false, err
		}
	}
	var balance billingschema.BillingBalanceSnapshot
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", account.AccountID).First(&balance).Error; err != nil {
		return false, err
	}
	if sub.AmountTotal <= 0 || balance.AvailableBalance < 0 || balance.ReservedBalance < 0 || balance.AvailableBalance > sub.AmountTotal || balance.ReservedBalance > sub.AmountTotal-balance.AvailableBalance {
		return false, errors.New("subscription ledger balance is inconsistent")
	}
	// amount_used includes outstanding holds. A refund can only return settled
	// consumption, never another request's reserved funds.
	sub.AmountUsed = sub.AmountTotal - balance.AvailableBalance
	if delta < 0 && -delta > sub.AmountUsed-balance.ReservedBalance {
		return false, errors.New("subscription refund exceeds settled usage")
	}
	if delta > 0 && delta > balance.AvailableBalance {
		return false, billingdomain.ErrInsufficientBalance
	}
	if err := applySubscriptionUsageDelta(plan, sub, modelName, delta); err != nil {
		return false, err
	}
	entry, err := billingdomain.AdjustAvailableBalanceTx(tx, billingdomain.AdjustAvailableBalanceParams{
		AccountID: account.AccountID, UsageAmount: delta, IdempotencyKey: key,
		ReasonCode: "subscription_usage_adjustment", ReferenceType: "user_subscription", ReferenceID: fmt.Sprint(sub.Id),
	})
	if err != nil {
		return false, err
	}
	metadata, err := json.Marshal(map[string]any{"delta": delta, "model": modelName})
	if err != nil {
		return false, err
	}
	if err := tx.Model(entry).Update("metadata", metadata).Error; err != nil {
		return false, err
	}
	if err := tx.Save(sub).Error; err != nil {
		return false, err
	}
	return true, nil
}
