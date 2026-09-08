package settlement

import (
	"fmt"
	"sync"
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
		ConsumerUserID: 20, BillingSource: "wallet", ConsumerDebitAmount: 100,
		SettlementGrossAmount: 100, WalletMultiplier: 1,
	}

	require.NoError(t, Record(params))
	require.NoError(t, Record(params))

	var item marketplaceschema.Settlement
	require.NoError(t, db.First(&item, "request_id = ?", params.RequestID).Error)
	require.Equal(t, int64(5), item.PlatformCommission)
	require.Equal(t, int64(100), item.ConsumerAmount)
	require.Equal(t, int64(100), item.SettlementGrossAmount)
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
		ConsumerUserID: 10, BillingSource: "wallet", ConsumerDebitAmount: 100,
		SettlementGrossAmount: 100, WalletMultiplier: 1,
	}))

	var item marketplaceschema.Settlement
	require.NoError(t, db.First(&item, "request_id = ?", "self").Error)
	require.Equal(t, int64(5), item.PlatformCommission)
	require.Equal(t, int64(95), item.OwnerNetAmount)
}

func TestReclaimPendingCreditsAdministratorAndMarksRecord(t *testing.T) {
	db := openSettlementTestDB(t)
	params := RecordParams{RequestID: "reclaim", GroupID: "group", OwnerUserID: 10, ConsumerUserID: 20, ConsumerDebitAmount: 100, SettlementGrossAmount: 100, WalletMultiplier: 1}
	require.NoError(t, Record(params))
	var recorded marketplaceschema.Settlement
	require.NoError(t, db.First(&recorded, "request_id = ?", params.RequestID).Error)
	require.NoError(t, db.Model(&recorded).Update("status", statusReleased).Error)
	var creditedUser, creditedAmount int
	RegisterReclaimHook(func(_ *gorm.DB, userID int, adminID int, amount int, _ string) error {
		creditedUser, creditedAmount = userID, amount
		require.Equal(t, 1, adminID)
		return nil
	})
	t.Cleanup(func() { RegisterReclaimHook(nil) })
	result, err := ReclaimPending(ReleaseFilter{})
	require.NoError(t, err)
	require.Equal(t, 1, result.Count)
	require.EqualValues(t, 95, result.Amount)
	require.Equal(t, 10, creditedUser)
	require.Equal(t, 95, creditedAmount)
	var item marketplaceschema.Settlement
	require.NoError(t, db.First(&item, "request_id = ?", params.RequestID).Error)
	require.Equal(t, statusReclaimed, item.Status)
	require.NotNil(t, item.ReclaimedAt)
}

func TestReclaimAmountSmallerThanOneSettlement(t *testing.T) {
	db := openSettlementTestDB(t)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{
		RequestID: "partial", GroupID: "group", OwnerUserID: 10,
		OwnerNetAmount: 95, Status: statusReleased,
	}).Error)
	var debited int
	RegisterReclaimHook(func(_ *gorm.DB, _, _ int, amount int, _ string) error {
		debited += amount
		return nil
	})
	t.Cleanup(func() { RegisterReclaimHook(nil) })
	filter := ReleaseFilter{OwnerUserIDs: []int{10}, MaxAmount: 40, OperationID: "partial-action"}
	result, err := ReclaimPending(filter)
	require.NoError(t, err)
	require.EqualValues(t, 40, result.Amount)
	require.Equal(t, 40, debited)
	var item marketplaceschema.Settlement
	require.NoError(t, db.First(&item, "request_id = ?", "partial").Error)
	require.EqualValues(t, 95, item.OwnerNetAmount)
	require.EqualValues(t, 40, item.ReclaimedAmount)
	require.Equal(t, statusReleased, item.Status)
	repeated, err := ReclaimPending(filter)
	require.NoError(t, err)
	require.Equal(t, result, repeated)
	require.Equal(t, 40, debited)
	filter.MaxAmount = 1
	_, err = ReclaimPending(filter)
	require.ErrorContains(t, err, "操作标识")
	rest, err := ReclaimPending(ReleaseFilter{OwnerUserIDs: []int{10}, OperationID: "reclaim-rest"})
	require.NoError(t, err)
	require.EqualValues(t, 55, rest.Amount)
	require.Equal(t, 95, debited)
	require.NoError(t, db.First(&item, "request_id = ?", "partial").Error)
	require.EqualValues(t, 95, item.ReclaimedAmount)
	require.Equal(t, statusReclaimed, item.Status)
	var count int64
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestRecordSubscriptionSettlementUsesWalletGrossForOwnerIncome(t *testing.T) {
	db := openSettlementTestDB(t)
	require.NoError(t, Record(RecordParams{
		RequestID: "subscription", GroupID: "group", OwnerUserID: 10,
		ConsumerUserID: 20, BillingSource: "subscription", ConsumerDebitAmount: 600,
		SettlementGrossAmount: 60, WalletMultiplier: 0.06, SubscriptionMultiplier: 0.6,
	}))

	var item marketplaceschema.Settlement
	require.NoError(t, db.First(&item, "request_id = ?", "subscription").Error)
	require.Equal(t, "subscription", item.BillingSource)
	require.Equal(t, int64(600), item.ConsumerAmount)
	require.Equal(t, int64(60), item.SettlementGrossAmount)
	require.Equal(t, int64(3), item.PlatformCommission)
	require.Equal(t, int64(57), item.OwnerNetAmount)
	require.Equal(t, 0.06, item.Multiplier)
	require.Equal(t, 0.6, item.SubscriptionMultiplier)
}

func TestRecordAllowsZeroRoundedPlatformCommission(t *testing.T) {
	db := openSettlementTestDB(t)
	require.NoError(t, Record(RecordParams{
		RequestID: "small", GroupID: "group", OwnerUserID: 10,
		ConsumerUserID: 20, BillingSource: "wallet", ConsumerDebitAmount: 1,
		SettlementGrossAmount: 1, WalletMultiplier: 1,
	}))
	var item marketplaceschema.Settlement
	require.NoError(t, db.First(&item, "request_id = ?", "small").Error)
	require.Zero(t, item.PlatformCommission)
	require.Equal(t, int64(1), item.OwnerNetAmount)
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
		&marketplaceschema.IncomeReclaim{},
	))
	return db
}

func TestReclaimAllIncludesMoreThanFiveThousandRecordsAndRespectsFilters(t *testing.T) {
	db := openSettlementTestDB(t)
	// Match the timestamp convention used by GORM's CreatedAt writer.
	reference := db.NowFunc().Truncate(time.Second)
	items := make([]marketplaceschema.Settlement, 5001)
	for i := range items {
		items[i] = marketplaceschema.Settlement{ID: fmt.Sprintf("item-%05d", i), RequestID: fmt.Sprintf("item-%05d", i), GroupID: "g", OwnerUserID: 10, OwnerNetAmount: 1, Status: statusReleased, CreatedAt: reference}
	}
	require.NoError(t, db.CreateInBatches(items, 100).Error)
	require.NoError(t, db.Create([]marketplaceschema.Settlement{
		{RequestID: "outside-owner", GroupID: "g", OwnerUserID: 11, OwnerNetAmount: 100, Status: statusReleased, CreatedAt: reference},
		{RequestID: "outside-time", GroupID: "g", OwnerUserID: 10, OwnerNetAmount: 100, Status: statusReleased, CreatedAt: reference.Add(-time.Hour)},
		{RequestID: "pending", GroupID: "g", OwnerUserID: 10, OwnerNetAmount: 100, Status: statusPending, CreatedAt: reference},
	}).Error)
	transfers := 0
	RegisterReclaimHook(func(_ *gorm.DB, owner, _ int, amount int, _ string) error {
		transfers++
		require.Equal(t, 10, owner)
		require.Equal(t, 5001, amount)
		return nil
	})
	t.Cleanup(func() { RegisterReclaimHook(nil) })
	result, err := ReclaimPending(ReleaseFilter{OwnerUserIDs: []int{10}, StartTimestamp: reference.Unix(), EndTimestamp: reference.Unix(), OperationID: "large-batch"})
	require.NoError(t, err)
	require.Equal(t, 5001, result.Count)
	require.Equal(t, 1, transfers)
	require.EqualValues(t, 5001, result.Amount)
	var untouched int64
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Where("status <> ?", statusReclaimed).Count(&untouched).Error)
	require.EqualValues(t, 3, untouched)
}

func TestReclaimInsufficientEarningsRollsBackAndConcurrentRetryIsIdempotent(t *testing.T) {
	db := openSettlementTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{RequestID: "limited", GroupID: "g", OwnerUserID: 10, OwnerNetAmount: 95, Status: statusReleased}).Error)
	RegisterReclaimHook(func(_ *gorm.DB, _, _ int, _ int, _ string) error { return nil })
	t.Cleanup(func() { RegisterReclaimHook(nil) })
	result, err := ReclaimPending(ReleaseFilter{MaxAmount: 96, OperationID: "over-limit"})
	require.ErrorContains(t, err, "可回收收益不足")
	require.Zero(t, result)
	var item marketplaceschema.Settlement
	require.NoError(t, db.First(&item).Error)
	require.Zero(t, item.ReclaimedAmount)
	var count int64
	require.NoError(t, db.Model(&marketplaceschema.IncomeReclaim{}).Count(&count).Error)
	require.Zero(t, count)
	var wg sync.WaitGroup
	results := make([]ReclaimResult, 4)
	errs := make([]error, 4)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = ReclaimPending(ReleaseFilter{MaxAmount: 40, OperationID: "same-operation"})
		}(i)
	}
	wg.Wait()
	for i := range results {
		require.NoError(t, errs[i])
		require.EqualValues(t, 40, results[i].Amount)
	}
	require.NoError(t, db.First(&item).Error)
	require.EqualValues(t, 40, item.ReclaimedAmount)
	require.NoError(t, db.Model(&marketplaceschema.IncomeReclaim{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
