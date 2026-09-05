package app

import (
	"errors"
	"fmt"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"strings"
	"time"

	commercestore "github.com/sh2001sh/new-api/internal/commerce/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func reserveSubscriptionLedgerTx(tx *gorm.DB, sub *commerceschema.UserSubscription, record *commerceschema.SubscriptionPreConsumeRecord) (*billingschema.BillingReservation, error) {
	if sub == nil || record == nil || sub.AmountTotal <= 0 {
		return nil, nil
	}
	account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
		AccountType: "subscription",
		OwnerType:   "user_subscription",
		OwnerID:     int64(sub.Id),
		QuotaUnit:   "quota",
	})
	if err != nil {
		return nil, err
	}

	ledgerBacked, err := subscriptionLedgerHasEntriesTx(tx, account.AccountID)
	if err != nil {
		return nil, err
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
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		sub := &commerceschema.UserSubscription{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", subscriptionID).First(sub).Error; err != nil {
			return err
		}
		now := commercestore.GetDBTimestamp()
		if sub.Status != "active" || sub.EndTime <= now {
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
			IdempotencyKey: fmt.Sprintf("subscription:%s:reserve-extra:%d", requestID, amount),
			ExpiresAt:      subscriptionReservationExpiry(),
		})
		return err
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
		if record.Status == "refunded" {
			return nil
		}
		if record.PreConsumed <= 0 {
			record.Status = "refunded"
			return tx.Save(record).Error
		}
		if err := lockUserSubscriptionTx(tx, record.UserSubscriptionId); err != nil {
			return err
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
