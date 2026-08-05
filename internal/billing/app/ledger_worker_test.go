package app

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRunLedgerWorkerBatchRebuildsSnapshotAndPublishesEvents(t *testing.T) {
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

	require.NoError(t, db.Model(&billingschema.BillingBalanceSnapshot{}).Where("account_id = ?", account.AccountID).Updates(map[string]any{"available_balance": 0, "consumed_total": 0}).Error)
	processed, err := RunLedgerWorkerBatch(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 3, processed)

	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	require.Equal(t, int64(750), snapshot.AvailableBalance)
	require.Equal(t, int64(250), snapshot.ConsumedTotal)
	require.Equal(t, int64(50), snapshot.RefundedTotal)

	var pending int64
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).Where("status = ?", billingschema.BillingOutboxStatusPending).Count(&pending).Error)
	require.Zero(t, pending)
}

func TestGroupLedgerOutboxEventsCollapsesSameAccount(t *testing.T) {
	batches := groupLedgerOutboxEvents([]billingschema.BillingOutboxEvent{
		{EventID: "event-1", AccountID: "account-a"},
		{EventID: "event-2", AccountID: "account-b"},
		{EventID: "event-3", AccountID: "account-a"},
	})

	require.Len(t, batches, 2)
	require.Equal(t, "account-a", batches[0].AccountID)
	require.Equal(t, []string{"event-1", "event-3"}, batches[0].EventIDs)
	require.Equal(t, "account-b", batches[1].AccountID)
	require.Equal(t, []string{"event-2"}, batches[1].EventIDs)
}
