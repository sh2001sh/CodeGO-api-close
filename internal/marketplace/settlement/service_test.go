package settlement

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRecordAndReleaseSettlementAreIdempotent(t *testing.T) {
	db := openSettlementTestDB(t)
	params := RecordParams{
		RequestID: "request-1", GroupID: "group-1", OwnerUserID: 10,
		ConsumerUserID: 20, Amount: 100, Multiplier: 1,
	}

	require.NoError(t, Record(params))
	require.NoError(t, Record(params))

	var item marketplaceschema.Settlement
	require.NoError(t, db.First(&item, "request_id = ?", params.RequestID).Error)
	require.Equal(t, int64(5), item.PlatformCommission)
	require.Zero(t, item.TransactionFee)
	require.Equal(t, int64(95), item.OwnerNetAmount)

	var settlementCount int64
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Count(&settlementCount).Error)
	require.Equal(t, int64(1), settlementCount)

	var releasedAmount int
	RegisterReleaseHook(func(_ *gorm.DB, userID int, amount int, _, _ string) error {
		require.Equal(t, params.OwnerUserID, userID)
		releasedAmount += amount
		return nil
	})
	t.Cleanup(func() { RegisterReleaseHook(nil) })
	require.NoError(t, db.Model(&item).Update("available_at", time.Now().UTC().Add(-time.Minute)).Error)

	require.NoError(t, ReleaseDue(10))
	require.NoError(t, ReleaseDue(10))
	require.Equal(t, 95, releasedAmount)

	require.NoError(t, db.First(&item, "request_id = ?", params.RequestID).Error)
	require.Equal(t, statusReleased, item.Status)
	require.NotNil(t, item.ReleasedAt)

	var pendingSnapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&pendingSnapshot, "account_id = ?", item.PendingAccountID).Error)
	require.Zero(t, pendingSnapshot.AvailableBalance)
	require.Equal(t, int64(95), pendingSnapshot.ConsumedTotal)
}

func TestRecordSelfConsumptionStillChargesCommission(t *testing.T) {
	db := openSettlementTestDB(t)
	require.NoError(t, Record(RecordParams{
		RequestID: "self", GroupID: "group", OwnerUserID: 10,
		ConsumerUserID: 10, Amount: 100, Multiplier: 1,
	}))

	var item marketplaceschema.Settlement
	require.NoError(t, db.First(&item, "request_id = ?", "self").Error)
	require.Equal(t, int64(5), item.PlatformCommission)
	require.Equal(t, int64(95), item.OwnerNetAmount)
}

func openSettlementTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
		platformdb.UsingPostgreSQL = originalPostgreSQL
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
		&marketplaceschema.Settlement{},
	))
	return db
}
