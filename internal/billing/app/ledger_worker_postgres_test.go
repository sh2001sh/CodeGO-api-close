package app

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
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
	delete(ledgerReconciliationState.lastByAccount, account.AccountID)
	ledgerReconciliationState.Unlock()
	_, err = processLedgerOutboxAccount(context.Background(), account.AccountID)
	require.NoError(t, err)
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&actual).Error)
	require.EqualValues(t, 1100, actual.AvailableBalance)

	// A failed read must leave events pending and must not mark the account checked.
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 50, IdempotencyKey: account.AccountID + "-before-failure"})
	require.NoError(t, err)
	ledgerReconciliationState.Lock()
	delete(ledgerReconciliationState.lastByAccount, account.AccountID)
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
