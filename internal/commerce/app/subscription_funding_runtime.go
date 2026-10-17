package app

import (
	"errors"
	"fmt"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"math"
	"strings"
	"time"

	commercestore "github.com/sh2001sh/new-api/internal/commerce/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// lockedLedgerAccount is reusable only after this transaction confirmed ledger
// entries and locked its canonical snapshot; nil retains the bootstrap path.
func reserveSubscriptionLedgerTx(tx *gorm.DB, sub *commerceschema.UserSubscription, record *commerceschema.SubscriptionPreConsumeRecord, lockedLedgerAccount *billingschema.BillingAccount) (*billingschema.BillingReservation, error) {
	if sub == nil || record == nil || sub.AmountTotal <= 0 {
		return nil, nil
	}
	account := lockedLedgerAccount
	ledgerBacked := account != nil
	if account == nil {
		var err error
		account, err = billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
			AccountType: "subscription",
			OwnerType:   "user_subscription",
			OwnerID:     int64(sub.Id),
			QuotaUnit:   "quota",
		})
		if err != nil {
			return nil, err
		}

		ledgerBacked, err = subscriptionLedgerHasEntriesTx(tx, account.AccountID)
		if err != nil {
			return nil, err
		}
	}
	if !ledgerBacked {
		available := sub.AmountTotal - sub.AmountUsed
		if available > 0 {
			if _, err := billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
				AccountID:      account.AccountID,
				Amount:         available,
				IdempotencyKey: fmt.Sprintf("subscription-bootstrap:%d", sub.Id),
				ReasonCode:     "subscription_balance_bootstrap",
				ReferenceType:  "user_subscription",
				ReferenceID:    fmt.Sprintf("%d", sub.Id),
				OperatorType:   "subscription_projection",
				OperatorID:     record.RequestId,
			}); err != nil {
				return nil, err
			}
		}
	}
	reservation, err := billingdomain.CreateReservationTx(tx, billingdomain.CreateReservationParams{
		AccountID:      account.AccountID,
		RequestID:      record.RequestId,
		ReservedAmount: record.PreConsumed,
		IdempotencyKey: "subscription:" + record.RequestId + ":reserve",
		ExpiresAt:      subscriptionReservationExpiry(),
	})
	return reservation, err
}

func subscriptionLedgerHasEntriesTx(tx *gorm.DB, accountID string) (bool, error) {
	if tx == nil || strings.TrimSpace(accountID) == "" {
		return false, nil
	}
	var entry billingschema.BillingLedgerEntry
	query := tx.Select("entry_id").Where("account_id = ?", accountID).Limit(1).Find(&entry)
	if query.Error != nil {
		return false, query.Error
	}
	return query.RowsAffected > 0, nil
}

// ReserveAdditionalSubscriptionQuota reserves a confirmed extra amount for a request.
// The subscription fields remain a query projection; ledger reservations enforce balance.
func ReserveAdditionalSubscriptionQuota(requestID string, subscriptionID int, modelName string, amount int64) error {
	if strings.TrimSpace(requestID) == "" || subscriptionID <= 0 || amount <= 0 {
		return errors.New("requestId, subscriptionId and amount are required")
	}
	unlimited := false
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		sub := &commerceschema.UserSubscription{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", subscriptionID).First(sub).Error; err != nil {
			return err
		}
		if sub.AmountTotal == 0 {
			unlimited = true
			return nil
		}
		now, err := commercestore.GetDBTimestampTx(tx)
		if err != nil {
			return err
		}
		if sub.Status != "active" || (sub.EndTime <= now && !(sub.CreditPackGrantedQuota > 0 && sub.EndTime == 0)) {
			return errors.New("subscription is no longer active")
		}
		plan, err := getSubscriptionPlanRecordTx(tx, sub.PlanId)
		if err != nil {
			return err
		}
		account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
			AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(subscriptionID), QuotaUnit: "quota",
		})
		if err != nil {
			return err
		}
		var snapshot billingschema.BillingBalanceSnapshot
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", account.AccountID).First(&snapshot).Error; err != nil {
			return err
		}
		if snapshot.AvailableBalance < amount {
			return billingdomain.ErrInsufficientBalance
		}
		if err := applySubscriptionUsageDelta(plan, sub, modelName, amount); err != nil {
			return fmt.Errorf("%w: %v", billingdomain.ErrInsufficientBalance, err)
		}
		if err := tx.Save(sub).Error; err != nil {
			return err
		}
		_, err = billingdomain.CreateReservationTx(tx, billingdomain.CreateReservationParams{
			AccountID: account.AccountID, RequestID: requestID, ReservedAmount: amount,
			IdempotencyKey: fmt.Sprintf("subscription:%s:reserve-extra:%d", requestID, sub.AmountUsed),
			ExpiresAt:      subscriptionReservationExpiry(),
		})
		return err
	})
	if err == nil && unlimited {
		return reserveUnlimitedSubscriptionQuota(requestID, subscriptionID, modelName, amount, false)
	}
	return err
}

// ReserveUnlimitedSubscriptionQuotaTarget extends a usage hold to an absolute
// target. Replaying the same target cannot charge usage a second time.
func ReserveUnlimitedSubscriptionQuotaTarget(requestID string, subscriptionID int, modelName string, target int64) error {
	return reserveUnlimitedSubscriptionQuota(requestID, subscriptionID, modelName, target, true)
}

func reserveUnlimitedSubscriptionQuota(requestID string, subscriptionID int, modelName string, amount int64, absolute bool) error {
	if strings.TrimSpace(requestID) == "" || subscriptionID <= 0 || amount <= 0 {
		return errors.New("requestId, subscriptionId and positive usage target are required")
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		// Settlement and cancellation use this same record -> subscription order.
		var record commerceschema.SubscriptionPreConsumeRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("request_id = ?", requestID).First(&record).Error; err != nil {
			return err
		}
		if record.UserSubscriptionId != subscriptionID || record.ModelName != strings.TrimSpace(modelName) || record.Status != "consumed" || record.PreConsumed < 0 {
			return errors.New("unlimited subscription usage hold is unavailable")
		}
		sub, err := lockUserSubscriptionRecordTx(tx, subscriptionID)
		if err != nil {
			return err
		}
		now, err := commercestore.GetDBTimestampTx(tx)
		if err != nil {
			return err
		}
		if sub.AmountTotal != 0 || sub.Status != "active" || sub.EndTime <= now {
			return errors.New("unlimited subscription is no longer active")
		}
		target := amount
		if !absolute {
			if amount > math.MaxInt64-record.PreConsumed {
				return errors.New("unlimited subscription usage target is out of range")
			}
			target += record.PreConsumed
		}
		if target <= record.PreConsumed {
			return nil
		}
		_, err = AdjustUserSubscriptionUsageDeltaTx(tx, subscriptionID, modelName, target-record.PreConsumed,
			fmt.Sprintf("relay-reserve:%s:%d", requestID, target))
		if err != nil {
			return err
		}
		record.PreConsumed = target
		return tx.Save(&record).Error
	})
}

func subscriptionReservationExpiry() *time.Time {
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	return &expiresAt
}

// RefundSubscriptionPreConsume refunds a previous subscription pre-consume idempotently.
func RefundSubscriptionPreConsume(requestID string) error {
	if strings.TrimSpace(requestID) == "" {
		return errors.New("requestId is empty")
	}

	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		record := &commerceschema.SubscriptionPreConsumeRecord{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("request_id = ?", requestID).
			First(record).Error; err != nil {
			return err
		}
		if record.Status == "refunded" || record.Status == "settled" {
			return nil
		}
		if record.PreConsumed <= 0 {
			record.Status = "refunded"
			return tx.Save(record).Error
		}
		sub, err := lockUserSubscriptionRecordTx(tx, record.UserSubscriptionId)
		if err != nil {
			return err
		}
		if sub.AmountTotal == 0 {
			if _, err := AdjustUserSubscriptionUsageDeltaTx(tx, sub.Id, record.ModelName, -record.PreConsumed, "relay-cancel:"+requestID); err != nil {
				return err
			}
			record.Status = "refunded"
			return tx.Save(record).Error
		}
		releasedAmount, err := releaseSubscriptionReservationTx(tx, record)
		if err != nil {
			return err
		}
		if err := postConsumeUserSubscriptionUsageDeltaTx(tx, record.UserSubscriptionId, record.ModelName, -releasedAmount); err != nil {
			return err
		}
		record.Status = "refunded"
		return tx.Save(record).Error
	})
}
