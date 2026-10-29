package app

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
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
	ledgerReconciliationState.Lock()
	ledgerReconciliationState.lastByAccount[account.AccountID] = time.Now().Add(-ledgerReconciliationInterval)
	ledgerReconciliationState.nextAllowed = time.Time{}
	ledgerReconciliationState.Unlock()
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

func TestPublishNoopLedgerOutboxBatchLeavesFinancialEventsPending(t *testing.T) {
	db, _ := referralTestDB(t)
	now := time.Now().UTC()
	events := []billingschema.BillingOutboxEvent{
		{AccountID: "noop", EventType: "billing.credit_granted", IdempotencyKey: "noop-credit", CreatedAt: now},
		{AccountID: "noop", EventType: "billing.reservation_released", IdempotencyKey: "noop-release", CreatedAt: now.Add(time.Second)},
		{AccountID: "financial", EventType: "billing.reservation_settled", IdempotencyKey: "financial", CreatedAt: now.Add(2 * time.Second)},
		{AccountID: "future", EventType: "billing.future_event", IdempotencyKey: "future", CreatedAt: now.Add(3 * time.Second)},
	}
	require.NoError(t, db.Create(&events).Error)

	processed, err := publishNoopLedgerOutboxBatch(context.Background(), 100)
	require.NoError(t, err)
	require.EqualValues(t, 2, processed)

	for index := range events {
		require.NoError(t, db.First(&events[index], "event_id = ?", events[index].EventID).Error)
		if index < 2 {
			require.Equal(t, billingschema.BillingOutboxStatusPublished, events[index].Status)
			require.NotNil(t, events[index].PublishedAt)
			continue
		}
		require.Equal(t, billingschema.BillingOutboxStatusPending, events[index].Status)
		require.Nil(t, events[index].PublishedAt)
	}
}

func TestPublishProjectedMarketplaceUsageBatchLeavesMissingIncomePending(t *testing.T) {
	db, _ := referralTestDB(t)
	now := time.Now().UTC()
	receipt := marketplaceschema.Settlement{
		ID: "existing-income", RequestID: "projected-request", GroupID: "group",
		OwnerUserID: 1, ConsumerUserID: 2, OwnerNetAmount: 95, Status: "pending",
	}
	require.NoError(t, db.Create(&receipt).Error)
	events := []billingschema.BillingOutboxEvent{
		{AccountID: "projected", AggregateID: receipt.RequestID, EventType: marketplaceUsageSettledEvent, IdempotencyKey: "projected", CreatedAt: now},
		{AccountID: "missing", AggregateID: "missing-request", EventType: marketplaceUsageSettledEvent, IdempotencyKey: "missing", CreatedAt: now.Add(time.Second)},
		{AccountID: "referral", AggregateID: "native-settlement", EventType: "billing.reservation_settled", IdempotencyKey: "referral", CreatedAt: now.Add(2 * time.Second)},
	}
	require.NoError(t, db.Create(&events).Error)

	processed, err := publishProjectedMarketplaceUsageBatch(context.Background(), 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, processed)

	for index := range events {
		require.NoError(t, db.First(&events[index], "event_id = ?", events[index].EventID).Error)
		if index == 0 {
			require.Equal(t, billingschema.BillingOutboxStatusPublished, events[index].Status)
			require.NotNil(t, events[index].PublishedAt)
			continue
		}
		require.Equal(t, billingschema.BillingOutboxStatusPending, events[index].Status)
		require.Nil(t, events[index].PublishedAt)
	}
}

func TestLedgerReconciliationDueUsesPerAccountInterval(t *testing.T) {
	now := time.Now().UTC()
	accountID := "reconcile-interval-test"
	ledgerReconciliationState.Lock()
	delete(ledgerReconciliationState.lastByAccount, accountID)
	ledgerReconciliationState.nextAllowed = time.Time{}
	ledgerReconciliationState.Unlock()
	require.False(t, ledgerReconciliationDue(accountID, now), "cold accounts must not scan their full history")
	require.False(t, ledgerReconciliationDue(accountID, now.Add(ledgerReconciliationInterval-time.Second)))
	require.True(t, ledgerReconciliationDue(accountID, now.Add(ledgerReconciliationInterval)))
	require.False(t, ledgerReconciliationDue("another-account", now.Add(ledgerReconciliationInterval)), "cold accounts are initialized without scanning")
	markLedgerReconciled("another-account", now.Add(-ledgerReconciliationInterval))
	require.False(t, ledgerReconciliationDue("another-account", now.Add(ledgerReconciliationInterval)), "global spacing must bound scans")
	require.True(t, ledgerReconciliationDue("another-account", now.Add(ledgerReconciliationInterval+ledgerReconciliationMinSpacing)))
}
