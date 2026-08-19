package domain

import (
	"fmt"
	"strings"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	"gorm.io/gorm"
)

// AdjustAvailableBalanceParams describes an immediate quota consumption or
// refund that does not need a request-scoped reservation lifecycle.
type AdjustAvailableBalanceParams struct {
	AccountID      string
	UsageAmount    int64
	IdempotencyKey string
	ReasonCode     string
	ReferenceType  string
	ReferenceID    string
}

// AdjustAvailableBalanceTx atomically records an immediate quota consumption
// (positive UsageAmount) or refund (negative UsageAmount).
func AdjustAvailableBalanceTx(tx *gorm.DB, params AdjustAvailableBalanceParams) (*billingschema.BillingLedgerEntry, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction is required")
	}
	if strings.TrimSpace(params.AccountID) == "" || strings.TrimSpace(params.IdempotencyKey) == "" {
		return nil, fmt.Errorf("account_id and idempotency_key are required")
	}
	if params.UsageAmount == 0 {
		return nil, nil
	}

	entryType := LedgerEntryTypeSettleDebit
	direction := billingschema.BillingDirectionDebit
	amount := params.UsageAmount
	if params.UsageAmount < 0 {
		amount = -params.UsageAmount
		entryType = LedgerEntryTypeSettleCredit
		direction = billingschema.BillingDirectionCredit
	}
	if existing, found, err := findLedgerEntryByIdempotency(tx, params.IdempotencyKey); err != nil {
		return nil, err
	} else if found {
		if existing.AccountID != params.AccountID || existing.Amount != amount || existing.EntryType != entryType {
			return nil, ErrLedgerConflict
		}
		return existing, nil
	}

	snapshot, err := ensureAndLockBalanceSnapshot(tx, params.AccountID)
	if err != nil {
		return nil, err
	}

	if params.UsageAmount > 0 {
		if snapshot.AvailableBalance < params.UsageAmount {
			return nil, ErrInsufficientBalance
		}
		snapshot.AvailableBalance -= params.UsageAmount
		snapshot.ConsumedTotal += params.UsageAmount
	} else {
		amount = -params.UsageAmount
		entryType = LedgerEntryTypeSettleCredit
		direction = billingschema.BillingDirectionCredit
		snapshot.AvailableBalance += amount
		snapshot.RefundedTotal += amount
	}
	if err := tx.Save(snapshot).Error; err != nil {
		return nil, err
	}

	balanceAfter := snapshot.AvailableBalance
	entry := &billingschema.BillingLedgerEntry{
		AccountID:      params.AccountID,
		ReferenceType:  defaultIfEmpty(strings.TrimSpace(params.ReferenceType), "quota"),
		ReferenceID:    defaultIfEmpty(strings.TrimSpace(params.ReferenceID), params.AccountID),
		EntryType:      entryType,
		Direction:      direction,
		Amount:         amount,
		BalanceAfter:   &balanceAfter,
		IdempotencyKey: strings.TrimSpace(params.IdempotencyKey),
		ReasonCode:     defaultIfEmpty(strings.TrimSpace(params.ReasonCode), "quota_adjustment"),
		OperatorType:   "system",
	}
	if err := tx.Create(entry).Error; err != nil {
		return nil, err
	}
	if err := RecordOutboxEvent(tx, OutboxEventInput{
		AccountID:      entry.AccountID,
		AggregateType:  "ledger_entry",
		AggregateID:    entry.EntryID,
		EventType:      "billing.balance_adjusted",
		IdempotencyKey: "outbox:" + entry.IdempotencyKey,
		Payload:        entry,
	}); err != nil {
		return nil, err
	}
	return entry, nil
}
