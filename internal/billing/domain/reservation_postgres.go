package domain

import (
	"encoding/json"
	"fmt"
	"strings"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

// These paths retain the caller's transaction and row-lock order. Only the
// writes after locking are batched; legacy projections remain in the caller.
func createReservationPostgresTx(tx *gorm.DB, params CreateReservationParams) (billingschema.BillingReservation, error) {
	var reservation billingschema.BillingReservation
	snapshot, err := lockReservationSnapshotPostgres(tx, params.AccountID)
	if err != nil {
		return reservation, err
	}
	// A concurrent transaction can have committed this key while we waited for
	// the account lock. Recheck with a fresh READ COMMITTED statement snapshot.
	if existing, found, err := findReservationByIdempotency(tx, params.IdempotencyKey); err != nil {
		return reservation, err
	} else if found {
		if existing.AccountID != params.AccountID || existing.ReservedAmount != params.ReservedAmount || existing.RequestID != strings.TrimSpace(params.RequestID) || existing.WorkflowID != strings.TrimSpace(params.WorkflowID) {
			return reservation, ErrLedgerConflict
		}
		return *existing, nil
	}
	if snapshot.AvailableBalance < params.ReservedAmount {
		return reservation, ErrInsufficientBalance
	}
	now := tx.NowFunc()
	reservation = billingschema.BillingReservation{
		ReservationID: platformruntime.GetUUID(), AccountID: params.AccountID,
		RequestID: strings.TrimSpace(params.RequestID), WorkflowID: strings.TrimSpace(params.WorkflowID),
		ReservedAmount: params.ReservedAmount, Status: billingschema.BillingReservationStatusOpen,
		IdempotencyKey: strings.TrimSpace(params.IdempotencyKey), ExpiresAt: params.ExpiresAt,
		CreatedAt: now, UpdatedAt: now,
	}
	snapshot.AvailableBalance -= params.ReservedAmount
	snapshot.ReservedBalance += params.ReservedAmount
	snapshot.UpdatedAt = now
	err = writeReservationPostgres(tx, snapshot, &reservation, false, "entry:"+reservation.IdempotencyKey, "reservation_hold", "outbox:"+reservation.IdempotencyKey)
	return reservation, err
}

func releaseReservationPostgresTx(tx *gorm.DB, current *billingschema.BillingReservation, params ReleaseReservationParams) (billingschema.BillingReservation, error) {
	snapshot, err := lockReservationSnapshotPostgres(tx, current.AccountID)
	if err != nil {
		return billingschema.BillingReservation{}, err
	}
	if snapshot.ReservedBalance < current.ReservedAmount {
		return billingschema.BillingReservation{}, fmt.Errorf("reserved balance underflow")
	}
	snapshot.AvailableBalance += current.ReservedAmount
	snapshot.ReservedBalance -= current.ReservedAmount
	snapshot.UpdatedAt = tx.NowFunc()
	current.Status = billingschema.BillingReservationStatusReleased
	current.UpdatedAt = snapshot.UpdatedAt
	err = writeReservationPostgres(tx, snapshot, current, true, strings.TrimSpace(params.IdempotencyKey), defaultIfEmpty(strings.TrimSpace(params.ReasonCode), "reservation_release"), "outbox:"+strings.TrimSpace(params.IdempotencyKey))
	return *current, err
}

func lockReservationSnapshotPostgres(tx *gorm.DB, accountID string) (*billingschema.BillingBalanceSnapshot, error) {
	var snapshot billingschema.BillingBalanceSnapshot
	result := tx.Raw("SELECT * FROM billing.balance_snapshots WHERE account_id = ? FOR UPDATE", accountID).Scan(&snapshot)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		// Preserve initialization for accounts whose snapshot does not yet exist.
		return ensureAndLockBalanceSnapshot(tx, accountID)
	}
	return &snapshot, nil
}

func writeReservationPostgres(tx *gorm.DB, snapshot *billingschema.BillingBalanceSnapshot, reservation *billingschema.BillingReservation, release bool, entryKey, reasonCode, eventKey string) error {
	payload, err := json.Marshal(reservation)
	if err != nil {
		return fmt.Errorf("marshal billing outbox payload: %w", err)
	}
	query := postgresCreateReservationWrites
	entryType, direction, eventType := LedgerEntryTypeReserveHold, billingschema.BillingDirectionDebit, "billing.reservation_created"
	if release {
		query = postgresReleaseReservationWrites
		entryType, direction, eventType = LedgerEntryTypeReserveRelease, billingschema.BillingDirectionCredit, "billing.reservation_released"
	}
	args := map[string]interface{}{
		"account_id": reservation.AccountID, "reservation_id": reservation.ReservationID,
		"available_balance": snapshot.AvailableBalance, "reserved_balance": snapshot.ReservedBalance,
		"now": snapshot.UpdatedAt, "request_id": reservation.RequestID, "workflow_id": reservation.WorkflowID,
		"amount": reservation.ReservedAmount, "status": reservation.Status, "reservation_key": reservation.IdempotencyKey,
		"expires_at": reservation.ExpiresAt, "entry_id": platformruntime.GetUUID(), "entry_type": entryType,
		"direction": direction, "entry_key": entryKey, "reason_code": reasonCode,
		"event_id": platformruntime.GetUUID(), "event_type": eventType, "event_key": eventKey,
		"event_account_id": strings.TrimSpace(reservation.AccountID), "payload": string(payload),
	}
	var observed billingschema.BillingOutboxEvent
	result := tx.Raw(query, args).Scan(&observed)
	if result.Error != nil {
		return result.Error
	}
	expected := billingschema.BillingOutboxEvent{AccountID: strings.TrimSpace(reservation.AccountID), AggregateID: reservation.ReservationID, EventType: eventType}
	if result.RowsAffected > 0 {
		return validateExistingOutboxEvent(observed, expected)
	}
	// ON CONFLICT can observe an insert committed after this statement's snapshot.
	// A fresh statement preserves RecordOutboxEvent's concurrent replay validation.
	return RecordOutboxEvent(tx, OutboxEventInput{
		AccountID: reservation.AccountID, AggregateType: "reservation", AggregateID: reservation.ReservationID,
		EventType: eventType, IdempotencyKey: eventKey, Payload: reservation,
	})
}

const postgresReservationSnapshotWrite = `WITH snapshot_updated AS (
	UPDATE billing.balance_snapshots
	SET available_balance = @available_balance, reserved_balance = @reserved_balance, updated_at = @now
	WHERE account_id = @account_id
	RETURNING account_id, available_balance
), reservation_written AS (
`

const postgresCreateReservationWrites = postgresReservationSnapshotWrite + `
	INSERT INTO billing.reservations
		(reservation_id, request_id, workflow_id, account_id, reserved_amount, status, idempotency_key, expires_at, created_at, updated_at)
	SELECT @reservation_id, @request_id, @workflow_id, account_id, @amount, @status, @reservation_key, @expires_at, @now, @now
	FROM snapshot_updated
	RETURNING reservation_id
` + postgresReservationEntryAndOutboxWrites

const postgresReleaseReservationWrites = postgresReservationSnapshotWrite + `
	UPDATE billing.reservations r
	SET status = @status, updated_at = @now
	FROM snapshot_updated s
	WHERE r.reservation_id = @reservation_id AND r.account_id = s.account_id
	RETURNING r.reservation_id
` + postgresReservationEntryAndOutboxWrites

const postgresReservationEntryAndOutboxWrites = `
), entry_written AS (
	INSERT INTO billing.ledger_entries
		(entry_id, account_id, reference_type, reference_id, entry_type, direction, amount, balance_after,
		 idempotency_key, reason_code, reason_detail, operator_type, operator_id, metadata, created_at)
	SELECT @entry_id, s.account_id, 'reservation', r.reservation_id, @entry_type, @direction, @amount, s.available_balance,
		@entry_key, @reason_code, '', 'system', '', '{}'::json, @now
	FROM snapshot_updated s CROSS JOIN reservation_written r
	RETURNING entry_id
), event_written AS (
	INSERT INTO billing.outbox_events
		(event_id, account_id, aggregate_type, aggregate_id, event_type, payload, idempotency_key,
		 status, attempts, last_error, published_at, created_at, updated_at)
	SELECT @event_id, @event_account_id, 'reservation', @reservation_id, @event_type, CAST(@payload AS json), @event_key,
		'pending', 0, '', NULL, @now, @now
	FROM entry_written
	ON CONFLICT (idempotency_key) DO NOTHING
	RETURNING account_id, aggregate_id, event_type
)
SELECT account_id, aggregate_id, event_type FROM event_written
UNION ALL
SELECT account_id, aggregate_id, event_type FROM billing.outbox_events
WHERE idempotency_key = @event_key AND NOT EXISTS (SELECT 1 FROM event_written)
`
