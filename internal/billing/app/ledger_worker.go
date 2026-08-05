package app

import (
	"context"
	"fmt"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ledgerWorkerBatchSize = 100
	ledgerWorkerInterval  = 15 * time.Second
)

// StartLedgerWorker begins asynchronous outbox processing for the ledger runtime.
func StartLedgerWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(ledgerWorkerInterval)
		defer ticker.Stop()
		for {
			if _, err := RunLedgerWorkerBatch(ctx, ledgerWorkerBatchSize); err != nil {
				platformobservability.SysError("ledger worker batch failed: " + err.Error())
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// RunLedgerWorkerBatch publishes pending ledger events and rebuilds affected snapshots.
func RunLedgerWorkerBatch(ctx context.Context, limit int) (int, error) {
	if platformdb.DB == nil {
		return 0, fmt.Errorf("primary database is not initialized")
	}
	if limit <= 0 {
		limit = ledgerWorkerBatchSize
	}

	var events []billingschema.BillingOutboxEvent
	if err := platformdb.DB.WithContext(ctx).
		Where("status = ?", billingschema.BillingOutboxStatusPending).
		Order("created_at asc, event_id asc").
		Limit(limit).
		Find(&events).Error; err != nil {
		return 0, err
	}

	processed := 0
	for _, batch := range groupLedgerOutboxEvents(events) {
		count, err := processLedgerOutboxAccountBatch(ctx, batch)
		if err != nil {
			if markErr := markLedgerOutboxBatchFailure(ctx, batch.EventIDs, err); markErr != nil {
				return processed, fmt.Errorf("process ledger account batch: %w; mark failure: %v", err, markErr)
			}
			continue
		}
		processed += count
	}
	return processed, nil
}

type ledgerOutboxAccountBatch struct {
	AccountID string
	EventIDs  []string
}

// groupLedgerOutboxEvents preserves the oldest-event order while collapsing
// repeated updates for one account into a single snapshot rebuild.
func groupLedgerOutboxEvents(events []billingschema.BillingOutboxEvent) []ledgerOutboxAccountBatch {
	batches := make([]ledgerOutboxAccountBatch, 0, len(events))
	indexes := make(map[string]int, len(events))
	for _, event := range events {
		if event.AccountID == "" || event.EventID == "" {
			continue
		}
		index, found := indexes[event.AccountID]
		if !found {
			indexes[event.AccountID] = len(batches)
			batches = append(batches, ledgerOutboxAccountBatch{AccountID: event.AccountID})
			index = len(batches) - 1
		}
		batches[index].EventIDs = append(batches[index].EventIDs, event.EventID)
	}
	return batches
}

// RebuildBalanceSnapshot recalculates one account projection from immutable ledger data.
func RebuildBalanceSnapshot(ctx context.Context, accountID string) error {
	if platformdb.DB == nil {
		return fmt.Errorf("primary database is not initialized")
	}
	if accountID == "" {
		return fmt.Errorf("account id is required")
	}
	return platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return rebuildBalanceSnapshotTx(tx, accountID)
	})
}

func processLedgerOutboxAccountBatch(ctx context.Context, batch ledgerOutboxAccountBatch) (int, error) {
	if batch.AccountID == "" || len(batch.EventIDs) == 0 {
		return 0, nil
	}

	processed := 0
	err := platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var events []billingschema.BillingOutboxEvent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("event_id IN ? AND status = ?", batch.EventIDs, billingschema.BillingOutboxStatusPending).
			Find(&events).Error; err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		if err := rebuildBalanceSnapshotTx(tx, batch.AccountID); err != nil {
			return err
		}

		eventIDs := make([]string, 0, len(events))
		for _, event := range events {
			eventIDs = append(eventIDs, event.EventID)
		}
		now := time.Now().UTC()
		result := tx.Model(&billingschema.BillingOutboxEvent{}).
			Where("event_id IN ? AND status = ?", eventIDs, billingschema.BillingOutboxStatusPending).
			Updates(map[string]any{
				"status":       billingschema.BillingOutboxStatusPublished,
				"published_at": &now,
				"last_error":   "",
			})
		if result.Error != nil {
			return result.Error
		}
		processed = int(result.RowsAffected)
		return nil
	})
	return processed, err
}

func markLedgerOutboxBatchFailure(ctx context.Context, eventIDs []string, cause error) error {
	if len(eventIDs) == 0 {
		return nil
	}
	return platformdb.DB.WithContext(ctx).Model(&billingschema.BillingOutboxEvent{}).
		Where("event_id IN ? AND status = ?", eventIDs, billingschema.BillingOutboxStatusPending).
		Updates(map[string]any{
			"attempts":   gorm.Expr("attempts + ?", 1),
			"last_error": cause.Error(),
		}).Error
}

func rebuildBalanceSnapshotTx(tx *gorm.DB, accountID string) error {
	var account billingschema.BillingAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("account_id = ?", accountID).
		First(&account).Error; err != nil {
		return err
	}

	snapshot, err := aggregateExpectedBalanceSnapshot(tx, accountID)
	if err != nil {
		return err
	}
	return tx.Save(&snapshot).Error
}

func snapshotDiffers(actual billingschema.BillingBalanceSnapshot, expected billingschema.BillingBalanceSnapshot) bool {
	return actual.AvailableBalance != expected.AvailableBalance ||
		actual.ReservedBalance != expected.ReservedBalance ||
		actual.ConsumedTotal != expected.ConsumedTotal ||
		actual.RefundedTotal != expected.RefundedTotal ||
		actual.GrantedTotal != expected.GrantedTotal
}
