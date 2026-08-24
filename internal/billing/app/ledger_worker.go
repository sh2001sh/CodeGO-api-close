package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ledgerWorkerAccountBatchSize   = 24
	ledgerWorkerInterval           = 2 * time.Second
	ledgerReconciliationInterval   = 30 * time.Minute
	ledgerOutboxCleanupInterval    = time.Minute
	staleReservationScanInterval   = time.Minute
	ledgerOutboxPublishedRetention = 72 * time.Hour
	ledgerOutboxCleanupBatchSize   = 5000
)

var ledgerReconciliationState = struct {
	sync.Mutex
	lastByAccount map[string]time.Time
}{lastByAccount: make(map[string]time.Time)}

// StartLedgerWorker begins asynchronous outbox processing for the ledger runtime.
func StartLedgerWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(ledgerWorkerInterval)
		defer ticker.Stop()
		var lastCleanup time.Time
		var lastStaleRecovery time.Time
		for {
			if _, err := RunLedgerWorkerBatch(ctx, ledgerWorkerAccountBatchSize); err != nil {
				platformobservability.SysError("ledger worker batch failed: " + err.Error())
			}
			now := time.Now().UTC()
			if lastStaleRecovery.IsZero() || now.Sub(lastStaleRecovery) >= staleReservationScanInterval {
				runStaleReservationRecovery(ctx, now)
				lastStaleRecovery = now
			}
			if lastCleanup.IsZero() || now.Sub(lastCleanup) >= ledgerOutboxCleanupInterval {
				if _, err := cleanupPublishedLedgerOutboxBatch(ctx, now, ledgerOutboxCleanupBatchSize); err != nil {
					platformobservability.SysError("ledger outbox cleanup failed: " + err.Error())
				}
				lastCleanup = now
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func runStaleReservationRecovery(ctx context.Context, now time.Time) {
	total := StaleReservationRecoveryResult{}
	for {
		result, err := RecoverStaleRelayReservations(ctx, now, staleReservationRecoveryBatch)
		total.Requests += result.Requests
		total.Failed += result.Failed
		total.Released += result.Released
		total.Settled += result.Settled
		total.Capped += result.Capped
		total.Amount += result.Amount
		if err != nil {
			platformobservability.SysError("stale reservation recovery failed: " + err.Error())
			break
		}
		if result.Requests < staleReservationRecoveryBatch {
			break
		}
	}
	if total.Requests > 0 || total.Failed > 0 {
		platformobservability.SysLog(fmt.Sprintf("stale reservation recovery: requests=%d failed=%d released=%d settled=%d capped=%d amount=%d", total.Requests, total.Failed, total.Released, total.Settled, total.Capped, total.Amount))
	}
}

// RunLedgerWorkerBatch rebuilds one snapshot per pending account and publishes all
// currently pending projection events for that account in the same transaction.
func RunLedgerWorkerBatch(ctx context.Context, limit int) (int, error) {
	if platformdb.DB == nil {
		return 0, fmt.Errorf("primary database is not initialized")
	}
	if limit <= 0 {
		limit = ledgerWorkerAccountBatchSize
	}

	accountIDs, err := pendingLedgerOutboxAccountIDs(ctx, limit)
	if err != nil {
		return 0, err
	}

	processed := 0
	for _, accountID := range accountIDs {
		count, err := processLedgerOutboxAccount(ctx, accountID)
		if err != nil {
			if markErr := markLedgerOutboxAccountFailure(ctx, accountID, err); markErr != nil {
				return processed, fmt.Errorf("process ledger account %s: %w; mark failure: %v", accountID, err, markErr)
			}
			continue
		}
		processed += count
	}
	return processed, nil
}

func pendingLedgerOutboxAccountIDs(ctx context.Context, limit int) ([]string, error) {
	accountIDs := make([]string, 0, limit)
	err := platformdb.DB.WithContext(ctx).
		Where("status = ?", billingschema.BillingOutboxStatusPending).
		Where("account_id <> ?", "").
		Model(&billingschema.BillingOutboxEvent{}).
		Select("account_id").
		Group("account_id").
		Order("MIN(created_at) asc, account_id asc").
		Limit(limit).
		Pluck("account_id", &accountIDs).Error
	return accountIDs, err
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

func processLedgerOutboxAccount(ctx context.Context, accountID string) (int, error) {
	if accountID == "" {
		return 0, nil
	}

	processed := 0
	now := time.Now().UTC()
	reconcile := ledgerReconciliationDue(accountID, now)
	err := platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if reconcile {
			if err := rebuildBalanceSnapshotTx(tx, accountID); err != nil {
				return err
			}
		}

		result := tx.Model(&billingschema.BillingOutboxEvent{}).
			Where("account_id = ? AND status = ?", accountID, billingschema.BillingOutboxStatusPending).
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
	if err == nil && reconcile {
		markLedgerReconciled(accountID, now)
	}
	return processed, err
}

func ledgerReconciliationDue(accountID string, now time.Time) bool {
	ledgerReconciliationState.Lock()
	defer ledgerReconciliationState.Unlock()
	last, found := ledgerReconciliationState.lastByAccount[accountID]
	return !found || now.Sub(last) >= ledgerReconciliationInterval
}

func markLedgerReconciled(accountID string, now time.Time) {
	ledgerReconciliationState.Lock()
	ledgerReconciliationState.lastByAccount[accountID] = now
	ledgerReconciliationState.Unlock()
}

func cleanupPublishedLedgerOutboxBatch(ctx context.Context, now time.Time, limit int) (int64, error) {
	if platformdb.DB == nil || limit <= 0 {
		return 0, nil
	}
	cutoff := now.Add(-ledgerOutboxPublishedRetention)
	ids := platformdb.DB.WithContext(ctx).Model(&billingschema.BillingOutboxEvent{}).
		Select("event_id").
		Where("status = ? AND published_at < ?", billingschema.BillingOutboxStatusPublished, cutoff).
		Order("published_at asc").
		Limit(limit)
	result := platformdb.DB.WithContext(ctx).
		Where("event_id IN (?)", ids).
		Delete(&billingschema.BillingOutboxEvent{})
	return result.RowsAffected, result.Error
}

func markLedgerOutboxAccountFailure(ctx context.Context, accountID string, cause error) error {
	if accountID == "" {
		return nil
	}
	return platformdb.DB.WithContext(ctx).Model(&billingschema.BillingOutboxEvent{}).
		Where("account_id = ? AND status = ?", accountID, billingschema.BillingOutboxStatusPending).
		Updates(map[string]any{
			"attempts":   gorm.Expr("attempts + ?", 1),
			"last_error": cause.Error(),
		}).Error
}

func rebuildBalanceSnapshotTx(tx *gorm.DB, accountID string) error {
	var current billingschema.BillingBalanceSnapshot
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("account_id = ?", accountID).
		First(&current).Error; err != nil {
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
