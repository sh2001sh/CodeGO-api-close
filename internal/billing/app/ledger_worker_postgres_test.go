package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestLedgerReconciliationDoesNotBlockConcurrentCreditPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_LEDGER_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_LEDGER_POSTGRES_DSN to a dedicated test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	oldDB, oldPG, oldSQLite := platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite
	platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = db, true, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = oldDB, oldPG, oldSQLite
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.AutoMigrate(&billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{}))
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "wallet", OwnerType: "user", OwnerID: time.Now().UnixNano()})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 1000, IdempotencyKey: account.AccountID + "-initial"})
	require.NoError(t, err)

	type reconcileContextKey struct{}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), reconcileContextKey{}, true), 10*time.Second)
	defer cancel()
	snapshotRead, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var snapshotWrites atomic.Int32
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:pause_reconciliation", func(tx *gorm.DB) {
		if tx.Statement.Table == "balance_snapshots" && tx.Statement.Context.Value(reconcileContextKey{}) == true {
			once.Do(func() {
				close(snapshotRead)
				select {
				case <-resume:
				case <-ctx.Done():
				}
			})
		}
	}))
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:count_reconciliation_writes", func(tx *gorm.DB) {
		if tx.Statement.Table == "balance_snapshots" && tx.Statement.Context.Value(reconcileContextKey{}) == true {
			snapshotWrites.Add(1)
		}
	}))
	done := make(chan error, 1)
	ledgerReconciliationState.Lock()
	ledgerReconciliationState.lastByAccount[account.AccountID] = time.Now().Add(-ledgerReconciliationInterval)
	ledgerReconciliationState.nextAllowed = time.Time{}
	ledgerReconciliationState.Unlock()
	go func() {
		_, err := processLedgerOutboxAccount(ctx, account.AccountID)
		done <- err
	}()
	select {
	case <-snapshotRead:
	case <-ctx.Done():
		t.Fatal("reconciliation did not read the snapshot")
	}
	creditErr := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL lock_timeout = '500ms'").Error; err != nil {
			return err
		}
		_, err := billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 100, IdempotencyKey: account.AccountID + "-concurrent"})
		return err
	})
	close(resume)
	reconcileErr := <-done
	require.NoError(t, creditErr, "normal reconciliation must not hold the balance lock during the history scan")
	require.NoError(t, reconcileErr)
	require.Zero(t, snapshotWrites.Load(), "a concurrent credit must not cause a false mismatch or stale snapshot overwrite")
	var actual billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&actual).Error)
	require.EqualValues(t, 1100, actual.AvailableBalance)
	require.EqualValues(t, 1100, actual.GrantedTotal)

	// A real inconsistency must still be repaired under the original write lock.
	require.NoError(t, db.Model(&actual).Update("available_balance", 0).Error)
	ledgerReconciliationState.Lock()
	ledgerReconciliationState.lastByAccount[account.AccountID] = time.Now().Add(-ledgerReconciliationInterval)
	ledgerReconciliationState.nextAllowed = time.Time{}
	ledgerReconciliationState.Unlock()
	_, err = processLedgerOutboxAccount(context.Background(), account.AccountID)
	require.NoError(t, err)
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&actual).Error)
	require.EqualValues(t, 1100, actual.AvailableBalance)

	// A failed read must leave events pending and must not mark the account checked.
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 50, IdempotencyKey: account.AccountID + "-before-failure"})
	require.NoError(t, err)
	ledgerReconciliationState.Lock()
	ledgerReconciliationState.lastByAccount[account.AccountID] = time.Now().Add(-ledgerReconciliationInterval)
	ledgerReconciliationState.nextAllowed = time.Time{}
	ledgerReconciliationState.Unlock()
	readErr := errors.New("reconciliation read failed")
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail_reconciliation_read", func(tx *gorm.DB) {
		if tx.Statement.Table == "balance_snapshots" && tx.Statement.Context.Value(reconcileContextKey{}) == true {
			tx.AddError(readErr)
		}
	}))
	_, err = processLedgerOutboxAccount(ctx, account.AccountID)
	require.ErrorIs(t, err, readErr)
	require.True(t, ledgerReconciliationDue(account.AccountID, time.Now().UTC()))
	var pending int64
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).Where("account_id = ? AND status = ?", account.AccountID, billingschema.BillingOutboxStatusPending).Count(&pending).Error)
	require.EqualValues(t, 1, pending)
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&actual).Error)
	require.EqualValues(t, 1150, actual.AvailableBalance)
}

func TestLedgerOutboxDoesNotHoldEarlierEventLockDuringLaterEventPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_LEDGER_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_LEDGER_POSTGRES_DSN to a dedicated test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	oldDB, oldPG, oldSQLite := platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite
	platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = db, true, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = oldDB, oldPG, oldSQLite
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.AutoMigrate(&billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{}))
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "wallet", OwnerType: "user", OwnerID: time.Now().UnixNano()})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 1000, IdempotencyKey: account.AccountID + ":credit"})
	require.NoError(t, err)
	var first billingschema.BillingOutboxEvent
	require.NoError(t, db.First(&first, "account_id = ?", account.AccountID).Error)
	second := billingschema.BillingOutboxEvent{
		AccountID: account.AccountID, AggregateID: "missing-" + account.AccountID,
		EventType: "billing.reservation_settled", IdempotencyKey: account.AccountID + ":failing-event",
		CreatedAt: first.CreatedAt.Add(time.Second),
	}
	require.NoError(t, db.Create(&second).Error)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	laterEvent, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	injected := errors.New("later event unavailable")
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:pause_later_event", func(tx *gorm.DB) {
		if tx.Statement.Table == "settlements" {
			once.Do(func() { close(laterEvent) })
			select {
			case <-resume:
			case <-ctx.Done():
			}
			tx.AddError(injected)
		}
	}))
	done := make(chan error, 1)
	go func() {
		_, err := processLedgerOutboxAccount(ctx, account.AccountID)
		done <- err
	}()
	select {
	case <-laterEvent:
	case <-ctx.Done():
		t.Fatal("worker did not reach the later event")
	}
	var published billingschema.BillingOutboxEvent
	probeErr := db.Transaction(func(tx *gorm.DB) error {
		return tx.Raw("SELECT * FROM billing.outbox_events WHERE event_id = ? FOR UPDATE NOWAIT", first.EventID).Scan(&published).Error
	})
	close(resume)
	workerErr := <-done
	require.NoError(t, probeErr, "the earlier event's row lock must be released while the next event is blocked")
	require.ErrorIs(t, workerErr, injected)
	require.Equal(t, billingschema.BillingOutboxStatusPublished, published.Status)
}

func TestMarketplaceOwnerCreditWaitsForUserBeforeLockingBalancePostgres(t *testing.T) {
	dsn := os.Getenv("TEST_LEDGER_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_LEDGER_POSTGRES_DSN to a dedicated test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	oldDB, oldPG, oldSQLite := platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite
	platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = db, true, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = oldDB, oldPG, oldSQLite
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingOutboxEvent{}, &billingschema.FundingLot{}))
	userID := int(time.Now().UnixNano()%1_000_000_000) + 1000
	require.NoError(t, db.Create(&identityschema.User{Id: userID, Username: fmt.Sprintf("owner-%d", userID), Password: "unused", AffCode: fmt.Sprintf("o%d", userID)}).Error)
	var account *billingschema.BillingAccount
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var ensureErr error
		account, ensureErr = ensureMirroredUserAccountTx(tx, userID, billingAccountTypeClaudeWallet, 0)
		return ensureErr
	}))

	holder := db.Begin()
	require.NoError(t, holder.Error)
	defer holder.Rollback()
	require.NoError(t, holder.Exec("SET LOCAL lock_timeout = '2s'").Error)
	require.NoError(t, holder.Exec("SELECT id FROM users WHERE id = ? FOR UPDATE", userID).Error)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	backendPID := make(chan int, 1)
	creditDone := make(chan error, 1)
	go func() {
		creditDone <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var pid int
			if err := tx.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
				return err
			}
			backendPID <- pid
			return CreditMarketplaceOwnerEarningsTx(tx, userID, 10, fmt.Sprintf("lock-order-credit-%d", userID), "marketplace_owner_release")
		})
	}()
	pid := <-backendPID
	for {
		var waitType string
		require.NoError(t, db.Raw("SELECT COALESCE(wait_event_type, '') FROM pg_stat_activity WHERE pid = ?", pid).Scan(&waitType).Error)
		if waitType == "Lock" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("credit transaction did not wait on the held user row")
		case <-time.After(10 * time.Millisecond):
		}
	}

	probeErr := holder.Exec("SELECT account_id FROM billing.balance_snapshots WHERE account_id = ? FOR UPDATE NOWAIT", account.AccountID).Error
	if probeErr == nil {
		require.NoError(t, holder.Commit().Error)
	} else {
		require.NoError(t, holder.Rollback().Error)
	}
	require.NoError(t, <-creditDone)
	require.NoError(t, probeErr, "a wallet writer waiting on users must not hold the balance snapshot lock")
}

func TestMarketplaceReferralWaitsForPendingBalanceBeforeLockingUserPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_LEDGER_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_LEDGER_POSTGRES_DSN to a dedicated test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	oldDB, oldPG, oldSQLite := platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite
	platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = db, true, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = oldDB, oldPG, oldSQLite
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS marketplace").Error)
	require.NoError(t, db.AutoMigrate(
		&identityschema.User{}, &billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{},
		&billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{},
		&billingschema.BillingOutboxEvent{}, &billingschema.FundingLot{}, &marketplaceschema.Settlement{},
	))

	unique := time.Now().UnixNano()
	userID := int(unique%1_000_000_000) + 1000
	requestID := fmt.Sprintf("referral-lock-order-%d", unique)
	require.NoError(t, db.Create(&identityschema.User{Id: userID, Username: fmt.Sprintf("lock-%d", userID), Password: "unused", AffCode: fmt.Sprintf("l%d", userID)}).Error)
	pending, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{
		AccountType: "marketplace_owner_pending", OwnerType: "user", OwnerID: int64(userID), QuotaUnit: "quota",
	})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{
		AccountID: pending.AccountID, Amount: 1000, IdempotencyKey: requestID + ":pending-credit",
	})
	require.NoError(t, err)
	params := marketplacesettlement.RecordParams{
		RequestID: requestID, GroupID: "lock-order", OwnerUserID: userID, ConsumerUserID: userID + 1,
		ConsumerDebitAmount: 1000, SettlementGrossAmount: 1000, CreditPackGrantedQuota: 1000,
		CreditPackPaidQuota: 900, CreditPackChannelShareBPS: 6000,
	}
	item := marketplaceschema.Settlement{
		ID: requestID, RequestID: requestID, GroupID: params.GroupID, OwnerUserID: params.OwnerUserID,
		ConsumerUserID: params.ConsumerUserID, ConsumerAmount: params.ConsumerDebitAmount,
		SettlementGrossAmount: params.SettlementGrossAmount, PlatformCommission: 50, OwnerNetAmount: 950,
		CreditPackGrantedQuota: params.CreditPackGrantedQuota, CreditPackPaidQuota: params.CreditPackPaidQuota,
		CreditPackChannelShareBPS: params.CreditPackChannelShareBPS, ReferralRebatePending: true,
		Status: "pending", PendingAccountID: pending.AccountID, AvailableAt: time.Now().UTC().Add(time.Hour),
	}
	require.NoError(t, db.Create(&item).Error)

	holder := db.Begin()
	require.NoError(t, holder.Error)
	defer holder.Rollback()
	require.NoError(t, holder.Exec("SET LOCAL lock_timeout = '2s'").Error)
	require.NoError(t, holder.Exec("SELECT account_id FROM billing.balance_snapshots WHERE account_id = ? FOR UPDATE", pending.AccountID).Error)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	backendPID := make(chan int, 1)
	referralDone := make(chan error, 1)
	go func() {
		referralDone <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var pid int
			if err := tx.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
				return err
			}
			backendPID <- pid
			locked, err := marketplacesettlement.LockCreditPackReferralTx(tx, params)
			if err != nil {
				return err
			}
			if err := CreditMarketplaceOwnerEarningsTx(tx, userID, 10, requestID+":inviter-credit", "referral_rebate"); err != nil {
				return err
			}
			return marketplacesettlement.DebitCreditPackReferralTx(tx, locked, fmt.Sprintf("n%d", userID), 10)
		})
	}()
	pid := <-backendPID
	for {
		var waitType string
		require.NoError(t, db.Raw("SELECT COALESCE(wait_event_type, '') FROM pg_stat_activity WHERE pid = ?", pid).Scan(&waitType).Error)
		if waitType == "Lock" {
			break
		}
		select {
		case err := <-referralDone:
			t.Fatalf("referral transaction completed before waiting on the pending balance: %v", err)
		case <-ctx.Done():
			t.Fatal("referral transaction did not wait on the held pending balance")
		case <-time.After(10 * time.Millisecond):
		}
	}

	probeErr := holder.Exec("SELECT id FROM users WHERE id = ? FOR UPDATE NOWAIT", userID).Error
	if probeErr == nil {
		require.NoError(t, holder.Commit().Error)
	} else {
		require.NoError(t, holder.Rollback().Error)
	}
	require.NoError(t, <-referralDone)
	require.NoError(t, probeErr, "a referral waiting on the pending balance must not already hold the user lock")
}

func TestPublishProjectedMarketplaceUsageBatchPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_LEDGER_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_LEDGER_POSTGRES_DSN to a dedicated test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	oldDB, oldPG, oldSQLite := platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite
	platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = db, true, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = oldDB, oldPG, oldSQLite
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS marketplace").Error)
	require.NoError(t, db.AutoMigrate(&billingschema.BillingOutboxEvent{}, &marketplaceschema.Settlement{}))

	unique := time.Now().UnixNano()
	projectedRequest := fmt.Sprintf("projected-%d", unique)
	missingRequest := fmt.Sprintf("missing-%d", unique)
	receipt := marketplaceschema.Settlement{
		ID: projectedRequest, RequestID: projectedRequest, GroupID: "postgres-batch", OwnerUserID: 1,
		ConsumerUserID: 2, SettlementGrossAmount: 100, OwnerNetAmount: 95, Status: "pending",
	}
	require.NoError(t, db.Create(&receipt).Error)
	events := []billingschema.BillingOutboxEvent{
		{AccountID: projectedRequest, AggregateID: projectedRequest, EventType: marketplaceUsageSettledEvent, IdempotencyKey: projectedRequest, CreatedAt: time.Now().UTC()},
		{AccountID: missingRequest, AggregateID: missingRequest, EventType: marketplaceUsageSettledEvent, IdempotencyKey: missingRequest, CreatedAt: time.Now().UTC().Add(time.Millisecond)},
	}
	require.NoError(t, db.Create(&events).Error)

	processed, err := publishProjectedMarketplaceUsageBatch(context.Background(), 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, processed)
	require.NoError(t, db.First(&events[0], "event_id = ?", events[0].EventID).Error)
	require.NoError(t, db.First(&events[1], "event_id = ?", events[1].EventID).Error)
	require.Equal(t, billingschema.BillingOutboxStatusPublished, events[0].Status)
	require.NotNil(t, events[0].PublishedAt)
	require.Equal(t, billingschema.BillingOutboxStatusPending, events[1].Status)
	require.Nil(t, events[1].PublishedAt)
}
