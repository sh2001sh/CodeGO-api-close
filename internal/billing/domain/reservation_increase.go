package domain

import (
	"fmt"
	"strings"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

type IncreaseReservationParams struct {
	ReservationID  string
	Amount         int64
	IdempotencyKey string
}

// IncreaseReservation adds funds to an open reservation atomically.
func IncreaseReservation(params IncreaseReservationParams) (*billingschema.BillingReservation, error) {
	if err := validateIncreaseReservationParams(params); err != nil {
		return nil, err
	}

	var reservation billingschema.BillingReservation
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var innerErr error
		reservation, innerErr = increaseReservationTx(tx, params)
		return innerErr
	})
	if err != nil {
		return nil, err
	}
	return &reservation, nil
}

// IncreaseReservationTx adds funds to an open reservation in the caller's transaction.
func IncreaseReservationTx(tx *gorm.DB, params IncreaseReservationParams) (*billingschema.BillingReservation, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	if err := validateIncreaseReservationParams(params); err != nil {
		return nil, err
	}
	reservation, err := increaseReservationTx(tx, params)
	if err != nil {
		return nil, err
	}
	return &reservation, nil
}

func validateIncreaseReservationParams(params IncreaseReservationParams) error {
	if strings.TrimSpace(params.ReservationID) == "" || params.Amount <= 0 || strings.TrimSpace(params.IdempotencyKey) == "" {
		return fmt.Errorf("reservation_id, amount and idempotency_key are required")
	}
	return nil
}

func increaseReservationTx(tx *gorm.DB, params IncreaseReservationParams) (billingschema.BillingReservation, error) {
	var reservation billingschema.BillingReservation
	current, err := lockReservation(tx, params.ReservationID)
	if err != nil {
		return reservation, err
	}
	if current.Status != billingschema.BillingReservationStatusOpen {
		return reservation, ErrReservationNotOpen
	}

	idempotencyKey := strings.TrimSpace(params.IdempotencyKey)
	if existing, found, err := findLedgerEntryByIdempotency(tx, idempotencyKey); err != nil {
		return reservation, err
	} else if found {
		if existing.ReferenceID != current.ReservationID || existing.EntryType != LedgerEntryTypeReserveHold || existing.Amount != params.Amount {
			return reservation, ErrLedgerConflict
		}
		return *current, nil
	}

	snapshot, err := ensureAndLockBalanceSnapshot(tx, current.AccountID)
	if err != nil {
		return reservation, err
	}
	if snapshot.AvailableBalance < params.Amount {
		return reservation, ErrInsufficientBalance
	}

	snapshot.AvailableBalance -= params.Amount
	snapshot.ReservedBalance += params.Amount
	if err := tx.Save(snapshot).Error; err != nil {
		return reservation, err
	}

	current.ReservedAmount += params.Amount
	if err := tx.Save(current).Error; err != nil {
		return reservation, err
	}

	balanceAfter := snapshot.AvailableBalance
	entry := billingschema.BillingLedgerEntry{
		AccountID:      current.AccountID,
		ReferenceType:  "reservation",
		ReferenceID:    current.ReservationID,
		EntryType:      LedgerEntryTypeReserveHold,
		Direction:      billingschema.BillingDirectionDebit,
		Amount:         params.Amount,
		BalanceAfter:   &balanceAfter,
		IdempotencyKey: idempotencyKey,
		ReasonCode:     "reservation_hold_increase",
		OperatorType:   "system",
	}
	if err := tx.Create(&entry).Error; err != nil {
		return reservation, err
	}
	if err := RecordOutboxEvent(tx, OutboxEventInput{
		AccountID:      current.AccountID,
		AggregateType:  "reservation",
		AggregateID:    current.ReservationID,
		EventType:      "billing.reservation_increased",
		IdempotencyKey: "outbox:" + entry.IdempotencyKey,
		Payload:        current,
	}); err != nil {
		return reservation, err
	}
	return *current, nil
}
