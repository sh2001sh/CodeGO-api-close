package app

import (
	"errors"
	"strings"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SettleSubscriptionReservation atomically settles the ledger reservation and updates
// the subscription usage projection to the confirmed upstream usage.
func SettleSubscriptionReservation(requestID string, subscriptionID int, modelName string, actualAmount int64) error {
	if strings.TrimSpace(requestID) == "" {
		return errors.New("requestId is empty")
	}
	if subscriptionID <= 0 {
		return errors.New("invalid userSubscriptionId")
	}
	if actualAmount < 0 {
		return errors.New("actual amount cannot be negative")
	}

	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		record := &commerceschema.SubscriptionPreConsumeRecord{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("request_id = ?", requestID).First(record).Error; err != nil {
			return err
		}
		if record.UserSubscriptionId != subscriptionID {
			return errors.New("subscription reservation ownership mismatch")
		}
		if record.Status == "refunded" {
			return errors.New("subscription pre-consume already refunded")
		}
		if err := lockUserSubscriptionTx(tx, record.UserSubscriptionId); err != nil {
			return err
		}
		return settleSubscriptionReservationTx(tx, record, actualAmount)
	})
}

func settleSubscriptionReservationTx(tx *gorm.DB, record *commerceschema.SubscriptionPreConsumeRecord, actualAmount int64) error {
	reservations, err := findOpenSubscriptionReservationsTx(tx, record.RequestId)
	if err != nil {
		return err
	}
	if len(reservations) == 0 {
		var settledCount int64
		if err := tx.Model(&billingschema.BillingReservation{}).
			Where("request_id = ? AND status = ?", record.RequestId, billingschema.BillingReservationStatusSettled).
			Count(&settledCount).Error; err != nil {
			return err
		}
		if settledCount > 0 {
			return nil
		}
		return errors.New("subscription ledger reservation is missing")
	}

	remaining := actualAmount
	reservedTotal := int64(0)
	for index, reservation := range reservations {
		reservedTotal += reservation.ReservedAmount
		settledAmount := reservation.ReservedAmount
		if remaining < settledAmount {
			settledAmount = remaining
		}
		if index == len(reservations)-1 && remaining > settledAmount {
			settledAmount = remaining
		}
		if _, err := billingdomain.SettleReservationTx(tx, billingdomain.SettleReservationParams{
			ReservationID:   reservation.ReservationID,
			UsageEvidenceID: record.RequestId,
			ActualAmount:    settledAmount,
			IdempotencyKey:  "subscription:" + record.RequestId + ":settle:" + reservation.ReservationID,
		}); err != nil {
			return err
		}
		remaining -= settledAmount
		if remaining < 0 {
			remaining = 0
		}
	}
	if remaining > 0 {
		return errors.New("subscription ledger reservation is insufficient")
	}
	if actualAmount != reservedTotal {
		return postConsumeUserSubscriptionUsageDeltaTx(tx, record.UserSubscriptionId, record.ModelName, actualAmount-reservedTotal)
	}
	return nil
}

func releaseSubscriptionReservationTx(tx *gorm.DB, record *commerceschema.SubscriptionPreConsumeRecord) (int64, error) {
	reservations, err := findOpenSubscriptionReservationsTx(tx, record.RequestId)
	if err != nil {
		return 0, err
	}
	if len(reservations) == 0 {
		return 0, errors.New("subscription ledger reservation is missing")
	}
	releasedAmount := int64(0)
	for _, reservation := range reservations {
		releasedAmount += reservation.ReservedAmount
		if _, err := billingdomain.ReleaseReservationTx(tx, billingdomain.ReleaseReservationParams{
			ReservationID:  reservation.ReservationID,
			IdempotencyKey: "subscription:" + record.RequestId + ":release:" + reservation.ReservationID,
			ReasonCode:     "relay_failed_before_settlement",
		}); err != nil {
			return 0, err
		}
	}
	return releasedAmount, nil
}

func findSubscriptionReservationTx(tx *gorm.DB, requestID string) (*billingschema.BillingReservation, error) {
	var reservation billingschema.BillingReservation
	err := tx.Where("idempotency_key = ?", "subscription:"+requestID+":reserve").First(&reservation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &reservation, nil
}

func findOpenSubscriptionReservationsTx(tx *gorm.DB, requestID string) ([]billingschema.BillingReservation, error) {
	var reservations []billingschema.BillingReservation
	if err := tx.Where("request_id = ? AND status = ?", requestID, billingschema.BillingReservationStatusOpen).
		Order("created_at asc, reservation_id asc").Find(&reservations).Error; err != nil {
		return nil, err
	}
	return reservations, nil
}

// PostConsumeUserSubscriptionDelta updates total subscription usage without model-specific usage.
func PostConsumeUserSubscriptionDelta(userSubscriptionID int, delta int64) error {
	return PostConsumeUserSubscriptionUsageDelta(userSubscriptionID, "", delta)
}

// PostConsumeUserSubscriptionUsageDelta applies a usage delta to a subscription.
func PostConsumeUserSubscriptionUsageDelta(userSubscriptionID int, modelName string, delta int64) error {
	if userSubscriptionID <= 0 {
		return errors.New("invalid userSubscriptionId")
	}
	if delta == 0 {
		return nil
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return postConsumeUserSubscriptionUsageDeltaTx(tx, userSubscriptionID, modelName, delta)
	})
}

func postConsumeUserSubscriptionUsageDeltaTx(tx *gorm.DB, userSubscriptionID int, modelName string, delta int64) error {
	if tx == nil {
		return errors.New("tx is nil")
	}
	sub, err := lockUserSubscriptionRecordTx(tx, userSubscriptionID)
	if err != nil {
		return err
	}
	plan, err := getSubscriptionPlanRecordTx(tx, sub.PlanId)
	if err != nil {
		return err
	}
	if err := applySubscriptionUsageDelta(plan, sub, modelName, delta); err != nil {
		return err
	}
	return tx.Save(sub).Error
}

func lockUserSubscriptionTx(tx *gorm.DB, userSubscriptionID int) error {
	_, err := lockUserSubscriptionRecordTx(tx, userSubscriptionID)
	return err
}

func lockUserSubscriptionRecordTx(tx *gorm.DB, userSubscriptionID int) (*commerceschema.UserSubscription, error) {
	sub := &commerceschema.UserSubscription{}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userSubscriptionID).First(sub).Error; err != nil {
		return nil, err
	}
	return sub, nil
}

// subscriptionLedgerAvailableQuotaTx returns the authoritative spendable quota
// for subscriptions that have completed their one-time ledger bootstrap. A
// zero-entry account is intentionally treated as legacy projection-backed until
// its first successful reservation creates the bootstrap credit.
func subscriptionLedgerAvailableQuotaTx(tx *gorm.DB, sub *commerceschema.UserSubscription) (int64, bool, error) {
	if tx == nil || sub == nil || sub.Id <= 0 || sub.AmountTotal <= 0 {
		return 0, false, nil
	}

	var account billingschema.BillingAccount
	err := tx.Where("account_type = ? AND owner_type = ? AND owner_id = ? AND quota_unit = ?",
		"subscription", "user_subscription", sub.Id, "quota").First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	var entryCount int64
	if err := tx.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ?", account.AccountID).Count(&entryCount).Error; err != nil {
		return 0, false, err
	}
	if entryCount == 0 {
		return 0, false, nil
	}

	var snapshot billingschema.BillingBalanceSnapshot
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", account.AccountID).First(&snapshot).Error; err != nil {
		return 0, false, err
	}
	if snapshot.ReservedBalance == 0 {
		targetUsed := sub.AmountTotal - snapshot.AvailableBalance
		if targetUsed < 0 {
			targetUsed = 0
		}
		if targetUsed > sub.AmountTotal {
			targetUsed = sub.AmountTotal
		}
		sub.AmountUsed = targetUsed
	}
	return snapshot.AvailableBalance, true, nil
}
