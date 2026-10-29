package app

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestReferralWorkerConcurrentBacklogPostgres(t *testing.T) {
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
	require.NoError(t, db.AutoMigrate(
		&billingschema.BillingAccount{}, &billingschema.BillingReservation{},
		&billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{},
	))

	accountCount := 64
	eventsPerAccount := 64
	timeout := 60 * time.Second
	if os.Getenv("RUN_LEDGER_LOAD_TEST") == "1" {
		accountCount = 500
		eventsPerAccount = 100
		timeout = 3 * time.Minute
	}
	unique := time.Now().UnixNano()
	createdAt := time.Now().UTC().Add(-time.Hour)
	accounts := make([]billingschema.BillingAccount, 0, accountCount)
	reservations := make([]billingschema.BillingReservation, 0, accountCount*eventsPerAccount)
	settlements := make([]billingschema.BillingSettlement, 0, accountCount*eventsPerAccount)
	events := make([]billingschema.BillingOutboxEvent, 0, accountCount*eventsPerAccount)
	for accountIndex := range accountCount {
		accountID := fmt.Sprintf("referral-load-%d-%02d", unique, accountIndex)
		accounts = append(accounts, billingschema.BillingAccount{
			AccountID: accountID, AccountType: "marketplace_owner_pending",
			OwnerType: "user", OwnerID: int64(unique + int64(accountIndex) + 1), QuotaUnit: "quota",
		})
		for eventIndex := range eventsPerAccount {
			id := fmt.Sprintf("%d-%02d-%02d", unique, accountIndex, eventIndex)
			reservationID := "reservation-" + id
			settlementID := "settlement-" + id
			reservations = append(reservations, billingschema.BillingReservation{
				ReservationID: reservationID, RequestID: "request-" + id, AccountID: accountID,
				ReservedAmount: 1, Status: billingschema.BillingReservationStatusSettled,
				IdempotencyKey: "reservation-idem-" + id, CreatedAt: createdAt, UpdatedAt: createdAt,
			})
			settlements = append(settlements, billingschema.BillingSettlement{
				SettlementID: settlementID, ReservationID: reservationID, ActualAmount: 1,
				Status:         billingschema.BillingSettlementStatusCompleted,
				IdempotencyKey: "marketplace-release:" + id + ":settle", SettledAt: createdAt, CreatedAt: createdAt,
			})
			events = append(events, billingschema.BillingOutboxEvent{
				EventID: "event-" + id, AccountID: accountID, AggregateType: "settlement",
				AggregateID: settlementID, EventType: "billing.reservation_settled",
				IdempotencyKey: "outbox-" + id, Status: billingschema.BillingOutboxStatusPending,
				CreatedAt: createdAt, UpdatedAt: createdAt,
			})
		}
	}
	require.NoError(t, db.CreateInBatches(&accounts, 200).Error)
	require.NoError(t, db.CreateInBatches(&reservations, 500).Error)
	require.NoError(t, db.CreateInBatches(&settlements, 500).Error)
	require.NoError(t, db.CreateInBatches(&events, 500).Error)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	started := time.Now()
	processed := 0
	for processed < len(events) {
		count, runErr := RunReferralLedgerWorkerBatch(ctx, ledgerReferralAccountBatchSize)
		require.NoError(t, runErr)
		require.Positive(t, count)
		processed += count
	}
	elapsed := time.Since(started)
	var pending, published int64
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).
		Where("event_id LIKE ? AND status = ?", fmt.Sprintf("event-%d-%%", unique), billingschema.BillingOutboxStatusPending).
		Count(&pending).Error)
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).
		Where("event_id LIKE ? AND status = ?", fmt.Sprintf("event-%d-%%", unique), billingschema.BillingOutboxStatusPublished).
		Count(&published).Error)
	require.Zero(t, pending)
	require.EqualValues(t, len(events), published)
	t.Logf("processed=%d elapsed=%s throughput=%.1f events/s", processed, elapsed, float64(processed)/elapsed.Seconds())
}
