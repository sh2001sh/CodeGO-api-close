package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ledgerWorkerAccountBatchSize   = 24
	ledgerWorkerEventBatchSize     = 32
	ledgerWorkerAccountConcurrency = 8
	ledgerWorkerCandidateFactor    = 512
	ledgerWorkerCandidateMax       = 16384
	ledgerReferralAccountBatchSize = 48
	ledgerReferralEventBatchSize   = 64
	ledgerReferralConcurrency      = 16
	ledgerReferralCandidateFactor  = 1024
	ledgerReferralCandidateMax     = 65536
	ledgerWorkerNoopBatchSize      = 10000
	ledgerNonReferralBatchSize     = 2000
	ledgerNonReferralAccountBatch  = 32
	ledgerWorkerNoopBusyDelay      = 250 * time.Millisecond
	ledgerWorkerInterval           = 2 * time.Second
	ledgerReferralBusyDelay        = 25 * time.Millisecond
	ledgerReconciliationInterval   = 24 * time.Hour
	ledgerReconciliationMinSpacing = time.Minute
	ledgerReconciliationTimeout    = 15 * time.Second
	ledgerOutboxCleanupInterval    = time.Minute
	staleReservationScanInterval   = time.Minute
	ledgerOutboxPublishedRetention = 72 * time.Hour
	ledgerOutboxCleanupBatchSize   = 5000
)

var ledgerWorkerNoopEventTypes = []string{
	"billing.adjustment_recorded",
	"billing.balance_adjusted",
	"billing.credit_granted",
	"billing.reservation_created",
	"billing.reservation_released",
	"billing.reservation_increased",
}

var ledgerReconciliationState = struct {
	sync.Mutex
	lastByAccount map[string]time.Time
	nextAllowed   time.Time
}{lastByAccount: make(map[string]time.Time)}

// StartLedgerWorker begins asynchronous outbox processing for the ledger runtime.
func StartLedgerWorker(ctx context.Context) {
	go runNoopLedgerOutboxWorker(ctx)
	go runNonReferralSettlementOutboxWorker(ctx)
	go runReferralLedgerOutboxWorker(ctx)
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

func runNonReferralSettlementOutboxWorker(ctx context.Context) {
	for {
		processed, err := publishNonReferralSettlementBatch(ctx, ledgerNonReferralBatchSize)
		if err != nil {
			platformobservability.SysError("publish non-referral settlement outbox batch: " + err.Error())
		}
		delay := ledgerWorkerInterval
		if err == nil && processed == ledgerNonReferralBatchSize {
			delay = ledgerWorkerNoopBusyDelay
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func runReferralLedgerOutboxWorker(ctx context.Context) {
	for {
		marketplaceProcessed, marketplaceErr := RunMarketplaceReferralLedgerWorkerBatch(ctx, ledgerReferralAccountBatchSize)
		if marketplaceErr != nil {
			platformobservability.SysError("marketplace referral ledger worker batch failed: " + marketplaceErr.Error())
		}
		processed, err := RunReferralLedgerWorkerBatch(ctx, ledgerReferralAccountBatchSize)
		if err != nil {
			platformobservability.SysError("referral ledger worker batch failed: " + err.Error())
		}
		delay := ledgerWorkerInterval
		if marketplaceErr != nil || err != nil || marketplaceProcessed > 0 || processed > 0 {
			delay = ledgerReferralBusyDelay
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func runNoopLedgerOutboxWorker(ctx context.Context) {
	for {
		noopProcessed, err := publishNoopLedgerOutboxBatch(ctx, ledgerWorkerNoopBatchSize)
		if err != nil {
			platformobservability.SysError("publish no-op ledger outbox batch: " + err.Error())
		}
		projectedProcessed, projectedErr := publishProjectedMarketplaceUsageBatch(ctx, ledgerWorkerNoopBatchSize)
		if projectedErr != nil {
			platformobservability.SysError("publish projected marketplace usage batch: " + projectedErr.Error())
		}
		delay := ledgerWorkerInterval
		if err == nil && projectedErr == nil && (noopProcessed == ledgerWorkerNoopBatchSize || projectedProcessed == ledgerWorkerNoopBatchSize) {
			delay = ledgerWorkerNoopBusyDelay
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
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

// RunLedgerWorkerBatch periodically checks pending accounts and publishes their
// projection events. Only inconsistent snapshots need a locked rebuild.
func RunLedgerWorkerBatch(ctx context.Context, limit int) (int, error) {
	if platformdb.DB == nil {
		return 0, fmt.Errorf("primary database is not initialized")
	}
	if limit <= 0 {
		limit = ledgerWorkerAccountBatchSize
	}

	var noopProcessed int64
	if !platformdb.UsingPostgreSQL {
		var err error
		noopProcessed, err = publishNoopLedgerOutboxBatch(ctx, ledgerWorkerNoopBatchSize)
		if err != nil {
			return 0, err
		}
		if platformdb.DB.Migrator().HasTable(&marketplaceschema.Settlement{}) {
			projected, err := publishProjectedMarketplaceUsageBatch(ctx, ledgerWorkerNoopBatchSize)
			if err != nil {
				return int(noopProcessed), err
			}
			noopProcessed += projected
		}
	}

	accountIDs, err := pendingLedgerOutboxAccountIDs(ctx, limit)
	if err != nil {
		return int(noopProcessed), err
	}

	processed := int(noopProcessed)
	if len(accountIDs) == 0 {
		return processed, nil
	}

	concurrency := ledgerWorkerAccountConcurrency
	if !platformdb.UsingPostgreSQL {
		concurrency = 1
	}
	concurrency = min(concurrency, len(accountIDs))
	type accountResult struct {
		count int
		err   error
	}
	jobs := make(chan string)
	results := make(chan accountResult, len(accountIDs))
	var workers sync.WaitGroup
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for accountID := range jobs {
				count, processErr := processLedgerOutboxAccount(ctx, accountID)
				if processErr != nil {
					if markErr := markLedgerOutboxAccountFailure(ctx, accountID, processErr); markErr != nil {
						processErr = fmt.Errorf("process ledger account %s: %w; mark failure: %v", accountID, processErr, markErr)
					} else {
						processErr = nil
					}
				}
				results <- accountResult{count: count, err: processErr}
			}
		}()
	}
	for _, accountID := range accountIDs {
		jobs <- accountID
	}
	close(jobs)
	workers.Wait()
	close(results)
	var firstError error
	for result := range results {
		processed += result.count
		if firstError == nil && result.err != nil {
			firstError = result.err
		}
	}
	return processed, firstError
}

// RunReferralLedgerWorkerBatch drains settlement events from a dedicated
// candidate window so unrelated outbox traffic cannot hide referral work.
func RunReferralLedgerWorkerBatch(ctx context.Context, limit int) (int, error) {
	return runReferralLedgerWorkerBatch(ctx, limit, false)
}

// RunMarketplaceReferralLedgerWorkerBatch prioritizes credit-pack marketplace
// settlements whose owner earnings cannot be released until referral allocation
// finishes. The generic referral worker remains responsible for wallet events
// that do not carry this context.
func RunMarketplaceReferralLedgerWorkerBatch(ctx context.Context, limit int) (int, error) {
	return runReferralLedgerWorkerBatch(ctx, limit, true)
}

func runReferralLedgerWorkerBatch(ctx context.Context, limit int, marketplaceOnly bool) (int, error) {
	if platformdb.DB == nil {
		return 0, fmt.Errorf("primary database is not initialized")
	}
	if limit <= 0 {
		limit = ledgerReferralAccountBatchSize
	}
	var accountIDs []string
	var err error
	if marketplaceOnly {
		accountIDs, err = pendingMarketplaceReferralOutboxAccountIDs(ctx, limit)
	} else {
		accountIDs, err = pendingReferralOutboxAccountIDs(ctx, limit)
	}
	if err != nil || len(accountIDs) == 0 {
		return 0, err
	}
	concurrency := min(ledgerReferralConcurrency, len(accountIDs))
	if !platformdb.UsingPostgreSQL {
		concurrency = 1
	}
	type accountResult struct {
		count int
		err   error
	}
	jobs := make(chan string)
	results := make(chan accountResult, len(accountIDs))
	var workers sync.WaitGroup
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for accountID := range jobs {
				count, processErr := processLedgerOutboxAccountEvents(ctx, accountID, "billing.reservation_settled", ledgerReferralEventBatchSize, marketplaceOnly)
				if processErr != nil {
					var markErr error
					if marketplaceOnly {
						markErr = markMarketplaceReferralOutboxAccountFailure(ctx, accountID, processErr)
					} else {
						markErr = markLedgerReferralOutboxAccountFailure(ctx, accountID, processErr)
					}
					if markErr != nil {
						processErr = fmt.Errorf("process referral ledger account %s: %w; mark failure: %v", accountID, processErr, markErr)
					} else {
						processErr = nil
					}
				}
				results <- accountResult{count: count, err: processErr}
			}
		}()
	}
	for _, accountID := range accountIDs {
		jobs <- accountID
	}
	close(jobs)
	workers.Wait()
	close(results)
	processed := 0
	var firstError error
	for result := range results {
		processed += result.count
		if firstError == nil && result.err != nil {
			firstError = result.err
		}
	}
	return processed, firstError
}

func pendingMarketplaceReferralOutboxAccountIDs(ctx context.Context, limit int) ([]string, error) {
	accountIDs := make([]string, 0, limit)
	candidateLimit := limit * ledgerReferralCandidateFactor
	if candidateLimit < limit || candidateLimit > ledgerReferralCandidateMax {
		candidateLimit = ledgerReferralCandidateMax
	}
	candidates := platformdb.DB.WithContext(ctx).
		Where("status = ? AND event_type = ?", billingschema.BillingOutboxStatusPending, "billing.reservation_settled").
		Where(marketplaceReferralContextPredicate()).
		Where("account_id <> ?", "").
		Model(&billingschema.BillingOutboxEvent{}).
		Select("account_id, attempts, created_at, event_id").
		Order("attempts asc, created_at asc, event_id asc").
		Limit(candidateLimit)
	err := platformdb.DB.WithContext(ctx).Table("(?) AS pending_marketplace_referral_candidates", candidates).
		Select("account_id").
		Group("account_id").
		Order("MAX(attempts) asc, MIN(created_at) asc, account_id asc").
		Limit(limit).
		Pluck("account_id", &accountIDs).Error
	return accountIDs, err
}

func marketplaceReferralContextPredicate() string {
	switch platformdb.DB.Dialector.Name() {
	case "postgres":
		return "payload::jsonb ? 'referral_context'"
	case "mysql":
		return "JSON_EXTRACT(payload, '$.referral_context') IS NOT NULL"
	default:
		return "json_type(payload, '$.referral_context') IS NOT NULL"
	}
}

func pendingReferralOutboxAccountIDs(ctx context.Context, limit int) ([]string, error) {
	accountIDs := make([]string, 0, limit)
	candidateLimit := limit * ledgerReferralCandidateFactor
	if candidateLimit < limit || candidateLimit > ledgerReferralCandidateMax {
		candidateLimit = ledgerReferralCandidateMax
	}
	candidates := platformdb.DB.WithContext(ctx).
		Where("status = ? AND event_type = ?", billingschema.BillingOutboxStatusPending, "billing.reservation_settled").
		Where("account_id <> ?", "").
		Model(&billingschema.BillingOutboxEvent{}).
		Select("account_id, attempts, created_at, event_id").
		Order("attempts asc, created_at asc, event_id asc").
		Limit(candidateLimit)
	err := platformdb.DB.WithContext(ctx).Table("(?) AS pending_referral_candidates", candidates).
		Select("account_id").
		Group("account_id").
		Order("MAX(attempts) asc, MIN(created_at) asc, account_id asc").
		Limit(limit).
		Pluck("account_id", &accountIDs).Error
	return accountIDs, err
}

func pendingLedgerOutboxAccountIDs(ctx context.Context, limit int) ([]string, error) {
	accountIDs := make([]string, 0, limit)
	candidateLimit := limit * ledgerWorkerCandidateFactor
	if candidateLimit < limit || candidateLimit > ledgerWorkerCandidateMax {
		candidateLimit = ledgerWorkerCandidateMax
	}
	candidates := platformdb.DB.WithContext(ctx).
		Where("status = ?", billingschema.BillingOutboxStatusPending).
		Where("account_id <> ?", "").
		Model(&billingschema.BillingOutboxEvent{}).
		Select("account_id, attempts, created_at, event_id").
		Order("created_at asc, event_id asc").
		Limit(candidateLimit)
	// Bound the fairness aggregation to the oldest indexed window instead of
	// grouping every pending event. MAX keeps a fresh event from resetting the
	// retry history of a broken account inside that window.
	err := platformdb.DB.WithContext(ctx).Table("(?) AS pending_candidates", candidates).
		Select("account_id").
		Group("account_id").
		Order("MAX(attempts) asc, MIN(created_at) asc, account_id asc").
		Limit(limit).
		Pluck("account_id", &accountIDs).Error
	return accountIDs, err
}

func publishNoopLedgerOutboxBatch(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	candidateLimit := limit * 5
	candidates := platformdb.DB.WithContext(ctx).
		Model(&billingschema.BillingOutboxEvent{}).
		Select("event_id, event_type").
		Where("status = ?", billingschema.BillingOutboxStatusPending).
		Order("created_at asc, event_id asc").
		Limit(candidateLimit)
	ids := platformdb.DB.WithContext(ctx).Table("(?) AS pending_candidates", candidates).
		Select("event_id").
		Where("event_type IN ?", ledgerWorkerNoopEventTypes).
		Limit(limit)
	now := time.Now().UTC()
	result := platformdb.DB.WithContext(ctx).Model(&billingschema.BillingOutboxEvent{}).
		Where("status = ?", billingschema.BillingOutboxStatusPending).
		Where("event_id IN (?)", ids).
		Updates(map[string]any{
			"status":       billingschema.BillingOutboxStatusPublished,
			"published_at": &now,
			"last_error":   "",
		})
	return result.RowsAffected, result.Error
}

// Marketplace owner pending accounts only receive release and reclaim ledger
// entries. Their settlement events cannot produce consumption rebates, so they
// can be published in bulk without bypassing wallet or subscription accounting.
func publishNonReferralSettlementBatch(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	accountTable := billingschema.BillingAccount{}.TableName()
	eventTable := billingschema.BillingOutboxEvent{}.TableName()
	reservationTable := billingschema.BillingReservation{}.TableName()
	settlementTable := billingschema.BillingSettlement{}.TableName()
	validAccountEvent := platformdb.DB.WithContext(ctx).Table(eventTable+" AS pending_account_event").
		Select("1").
		Joins("JOIN "+settlementTable+" AS account_settled ON account_settled.settlement_id = pending_account_event.aggregate_id").
		Joins("JOIN "+reservationTable+" AS account_reserved ON account_reserved.reservation_id = account_settled.reservation_id AND account_reserved.account_id = pending_account_event.account_id").
		Where("pending_account_event.account_id = account.account_id").
		Where("pending_account_event.status = ? AND pending_account_event.event_type = ?", billingschema.BillingOutboxStatusPending, "billing.reservation_settled")
	accountIDs := platformdb.DB.WithContext(ctx).Table(accountTable+" AS account").
		Select("account.account_id").
		Where("account.account_type = ? AND account.owner_type = ?", "marketplace_owner_pending", "user").
		Where("EXISTS (?)", validAccountEvent).
		Order("account.account_id asc").
		Limit(ledgerNonReferralAccountBatch)
	ids := platformdb.DB.WithContext(ctx).Table(eventTable+" AS pending_settlements").
		Select("pending_settlements.event_id").
		Joins("JOIN "+settlementTable+" AS settled ON settled.settlement_id = pending_settlements.aggregate_id").
		Joins("JOIN "+reservationTable+" AS reserved ON reserved.reservation_id = settled.reservation_id AND reserved.account_id = pending_settlements.account_id").
		Where("pending_settlements.status = ? AND pending_settlements.event_type = ?", billingschema.BillingOutboxStatusPending, "billing.reservation_settled").
		Where("pending_settlements.account_id IN (?)", accountIDs).
		Limit(limit)
	now := time.Now().UTC()
	result := platformdb.DB.WithContext(ctx).Model(&billingschema.BillingOutboxEvent{}).
		Where("status = ?", billingschema.BillingOutboxStatusPending).
		Where("event_id IN (?)", ids).
		Updates(map[string]any{
			"status":       billingschema.BillingOutboxStatusPublished,
			"published_at": &now,
			"last_error":   "",
		})
	return result.RowsAffected, result.Error
}

// Existing marketplace receipts prove that the inline projection committed.
// Publishing those durable repair intents in bulk keeps the per-event worker
// available for genuinely missing receipts and referral allocation.
func publishProjectedMarketplaceUsageBatch(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	candidateLimit := limit * 5
	candidates := platformdb.DB.WithContext(ctx).
		Model(&billingschema.BillingOutboxEvent{}).
		Select("event_id, aggregate_id").
		Where("status = ? AND event_type = ?", billingschema.BillingOutboxStatusPending, marketplaceUsageSettledEvent).
		Order("created_at asc, event_id asc").
		Limit(candidateLimit)
	settlementTable := marketplaceschema.Settlement{}.TableName()
	ids := platformdb.DB.WithContext(ctx).Table("(?) AS pending_candidates", candidates).
		Select("pending_candidates.event_id").
		Joins("JOIN " + settlementTable + " AS settlement ON settlement.request_id = pending_candidates.aggregate_id").
		Limit(limit)
	now := time.Now().UTC()
	result := platformdb.DB.WithContext(ctx).Model(&billingschema.BillingOutboxEvent{}).
		Where("status = ?", billingschema.BillingOutboxStatusPending).
		Where("event_id IN (?)", ids).
		Updates(map[string]any{
			"status":       billingschema.BillingOutboxStatusPublished,
			"published_at": &now,
			"last_error":   "",
		})
	return result.RowsAffected, result.Error
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

	now := time.Now().UTC()
	reconcile := ledgerReconciliationDue(accountID, now)
	repair := false
	if reconcile {
		checkCtx, cancel := context.WithTimeout(ctx, ledgerReconciliationTimeout)
		var checkErr error
		repair, checkErr = balanceSnapshotNeedsRepair(checkCtx, accountID)
		cancel()
		if checkErr != nil {
			if !errors.Is(checkErr, context.DeadlineExceeded) {
				markLedgerReconciliationRetryable(accountID, now)
				return 0, checkErr
			}
			// A timed-out projection check is defensive maintenance. It must not
			// stop pending ledger events from being published on a busy database.
			platformobservability.SysError(fmt.Sprintf("skip timed-out ledger reconciliation account=%s: %s", accountID, checkErr.Error()))
		}
	}
	if repair {
		if err := RebuildBalanceSnapshot(ctx, accountID); err != nil {
			return 0, err
		}
	}
	if reconcile {
		markLedgerReconciled(accountID, now)
	}

	return processLedgerOutboxAccountEvents(ctx, accountID, "", ledgerWorkerEventBatchSize, false)
}

func processLedgerOutboxAccountEvents(ctx context.Context, accountID string, eventType string, batchSize int, marketplaceReferralOnly bool) (int, error) {
	if accountID == "" || batchSize <= 0 {
		return 0, nil
	}
	processed := 0
	// Select a bounded batch, then commit each event with all of its financial
	// effects. Holding the snapshots of several owners until the whole batch
	// completes creates lock cycles with live settlement and income release.
	var events []billingschema.BillingOutboxEvent
	query := platformdb.DB.WithContext(ctx).
		Where("account_id = ? AND status = ?", accountID, billingschema.BillingOutboxStatusPending).
		Order("created_at asc, event_id asc").Limit(batchSize)
	if eventType != "" {
		query = query.Where("event_type = ?", eventType)
	}
	if marketplaceReferralOnly {
		query = query.Where(marketplaceReferralContextPredicate())
	}
	if err := query.Find(&events).Error; err != nil {
		return 0, err
	}
	for _, selected := range events {
		creditedInviters := make(map[int]struct{})
		published := false
		err := platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var event billingschema.BillingOutboxEvent
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("event_id = ? AND account_id = ? AND status = ?", selected.EventID, accountID, billingschema.BillingOutboxStatusPending).
				First(&event).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil // Another worker already committed this event.
			}
			if err != nil {
				return err
			}
			if err := consumeMarketplaceUsageTx(tx, event); err != nil {
				return err
			}
			if err := consumeReferralSettlementTx(tx, event, creditedInviters); err != nil {
				return err
			}
			publishedAt := time.Now().UTC()
			result := tx.Model(&billingschema.BillingOutboxEvent{}).
				Where("event_id = ? AND status = ?", event.EventID, billingschema.BillingOutboxStatusPending).
				Updates(map[string]any{
					"status":       billingschema.BillingOutboxStatusPublished,
					"published_at": &publishedAt,
					"last_error":   "",
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("ledger event %s publication changed while locked", event.EventID)
			}
			published = true
			return nil
		})
		if err != nil {
			return processed, err
		}
		if published {
			processed++
		}
		for inviterID := range creditedInviters {
			if cacheErr := identitystore.InvalidateUserCache(inviterID); cacheErr != nil {
				platformobservability.SysError(fmt.Sprintf("invalidate referral rebate recipient cache user=%d: %s", inviterID, cacheErr.Error()))
			}
		}
	}
	return processed, nil
}

func balanceSnapshotNeedsRepair(ctx context.Context, accountID string) (bool, error) {
	differs := false
	// Read the projection and ledger from one committed view without blocking
	// credits/reservations while scanning history. READ COMMITTED could compare
	// a snapshot from before a concurrent credit with ledger entries from after it.
	err := platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var actual billingschema.BillingBalanceSnapshot
		if err := tx.Where("account_id = ?", accountID).First(&actual).Error; err != nil {
			return err
		}
		expected, err := aggregateExpectedBalanceSnapshot(tx, accountID)
		if err != nil {
			return err
		}
		differs = snapshotDiffers(actual, expected)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	// A mismatch is only a signal: the repair transaction recomputes under the
	// original write lock, never writes this potentially outdated expected value.
	return differs, err
}

func ledgerReconciliationDue(accountID string, now time.Time) bool {
	ledgerReconciliationState.Lock()
	defer ledgerReconciliationState.Unlock()
	last, found := ledgerReconciliationState.lastByAccount[accountID]
	if !found {
		// A process restart must not turn the first event for every account into
		// a full immutable-ledger scan.
		ledgerReconciliationState.lastByAccount[accountID] = now
		return false
	}
	if now.Sub(last) < ledgerReconciliationInterval || now.Before(ledgerReconciliationState.nextAllowed) {
		return false
	}
	// Reserve the slot before starting the query. A timeout therefore cannot
	// cause the worker to retry the same expensive scan every two seconds.
	ledgerReconciliationState.lastByAccount[accountID] = now
	ledgerReconciliationState.nextAllowed = now.Add(ledgerReconciliationMinSpacing)
	return true
}

func markLedgerReconciled(accountID string, now time.Time) {
	ledgerReconciliationState.Lock()
	ledgerReconciliationState.lastByAccount[accountID] = now
	ledgerReconciliationState.Unlock()
}

func markLedgerReconciliationRetryable(accountID string, now time.Time) {
	ledgerReconciliationState.Lock()
	ledgerReconciliationState.lastByAccount[accountID] = now.Add(-ledgerReconciliationInterval)
	ledgerReconciliationState.nextAllowed = time.Time{}
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
		Where("event_type NOT IN ?", ledgerWorkerNoopEventTypes).
		Updates(map[string]any{
			"attempts":   gorm.Expr("attempts + ?", 1),
			"last_error": cause.Error(),
		}).Error
}

func markLedgerReferralOutboxAccountFailure(ctx context.Context, accountID string, cause error) error {
	if accountID == "" {
		return nil
	}
	return platformdb.DB.WithContext(ctx).Model(&billingschema.BillingOutboxEvent{}).
		Where("account_id = ? AND status = ? AND event_type = ?", accountID, billingschema.BillingOutboxStatusPending, "billing.reservation_settled").
		Updates(map[string]any{
			"attempts":   gorm.Expr("attempts + ?", 1),
			"last_error": cause.Error(),
		}).Error
}

func markMarketplaceReferralOutboxAccountFailure(ctx context.Context, accountID string, cause error) error {
	if accountID == "" {
		return nil
	}
	return platformdb.DB.WithContext(ctx).Model(&billingschema.BillingOutboxEvent{}).
		Where("account_id = ? AND status = ? AND event_type = ?", accountID, billingschema.BillingOutboxStatusPending, "billing.reservation_settled").
		Where(marketplaceReferralContextPredicate()).
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
