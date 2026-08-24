package app

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRunLedgerWorkerBatchRebuildsSnapshotAndPublishesEvents(t *testing.T) {
	originalDB := platformdb.DB
	originalUsingSQLite := platformdb.UsingSQLite
	originalUsingPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalUsingSQLite
		platformdb.UsingPostgreSQL = originalUsingPostgreSQL
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	platformdb.UsingSQLite = true
	platformdb.UsingPostgreSQL = false
	require.NoError(t, db.AutoMigrate(
		&billingschema.BillingAccount{},
		&billingschema.BillingBalanceSnapshot{},
		&billingschema.BillingLedgerEntry{},
		&billingschema.BillingReservation{},
		&billingschema.BillingSettlement{},
		&billingschema.BillingOutboxEvent{},
	))

	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "wallet", OwnerType: "user", OwnerID: 42})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 1000, IdempotencyKey: "credit-42"})
	require.NoError(t, err)
	reservation, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{AccountID: account.AccountID, RequestID: "request-42", ReservedAmount: 300, IdempotencyKey: "reserve-42"})
	require.NoError(t, err)
	_, err = billingdomain.SettleReservation(billingdomain.SettleReservationParams{ReservationID: reservation.ReservationID, ActualAmount: 250, IdempotencyKey: "settle-42"})
	require.NoError(t, err)
	secondAccount, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "wallet", OwnerType: "user", OwnerID: 43})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: secondAccount.AccountID, Amount: 500, IdempotencyKey: "credit-43"})
	require.NoError(t, err)

	require.NoError(t, db.Model(&billingschema.BillingBalanceSnapshot{}).Where("account_id = ?", account.AccountID).Updates(map[string]any{"available_balance": 0, "consumed_total": 0}).Error)
	processed, err := processLedgerOutboxAccount(context.Background(), account.AccountID)
	require.NoError(t, err)
	require.Equal(t, 3, processed)

	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	require.Equal(t, int64(750), snapshot.AvailableBalance)
	require.Equal(t, int64(250), snapshot.ConsumedTotal)
	require.Equal(t, int64(50), snapshot.RefundedTotal)

	var pending int64
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).Where("status = ?", billingschema.BillingOutboxStatusPending).Count(&pending).Error)
	require.Equal(t, int64(1), pending)

	processed, err = RunLedgerWorkerBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).Where("status = ?", billingschema.BillingOutboxStatusPending).Count(&pending).Error)
	require.Zero(t, pending)
}

func TestCleanupPublishedLedgerOutboxBatchPreservesRecentAndPendingEvents(t *testing.T) {
	originalDB := platformdb.DB
	originalUsingSQLite := platformdb.UsingSQLite
	originalUsingPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalUsingSQLite
		platformdb.UsingPostgreSQL = originalUsingPostgreSQL
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	platformdb.UsingSQLite = true
	platformdb.UsingPostgreSQL = false
	require.NoError(t, db.AutoMigrate(&billingschema.BillingOutboxEvent{}))

	now := time.Now().UTC()
	oldPublishedAt := now.Add(-ledgerOutboxPublishedRetention - time.Hour)
	recentPublishedAt := now.Add(-time.Hour)
	events := []billingschema.BillingOutboxEvent{
		{AccountID: "old", IdempotencyKey: "old", Status: billingschema.BillingOutboxStatusPublished, PublishedAt: &oldPublishedAt},
		{AccountID: "recent", IdempotencyKey: "recent", Status: billingschema.BillingOutboxStatusPublished, PublishedAt: &recentPublishedAt},
		{AccountID: "pending", IdempotencyKey: "pending", Status: billingschema.BillingOutboxStatusPending, PublishedAt: &oldPublishedAt},
	}
	require.NoError(t, db.Create(&events).Error)

	removed, err := cleanupPublishedLedgerOutboxBatch(context.Background(), now, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, removed)

	var remaining []billingschema.BillingOutboxEvent
	require.NoError(t, db.Order("account_id").Find(&remaining).Error)
	require.Len(t, remaining, 2)
	require.Equal(t, "pending", remaining[0].AccountID)
	require.Equal(t, "recent", remaining[1].AccountID)
}

func TestLedgerReconciliationDueUsesPerAccountInterval(t *testing.T) {
	now := time.Now().UTC()
	accountID := "reconcile-interval-test"
	require.True(t, ledgerReconciliationDue(accountID, now))
	markLedgerReconciled(accountID, now)
	require.False(t, ledgerReconciliationDue(accountID, now.Add(ledgerReconciliationInterval-time.Second)))
	require.True(t, ledgerReconciliationDue(accountID, now.Add(ledgerReconciliationInterval)))
}
