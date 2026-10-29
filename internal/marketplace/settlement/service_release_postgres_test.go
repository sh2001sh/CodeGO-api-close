package settlement

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestReleaseDueSerializesEachOwnerPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_RELEASE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_RELEASE_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var databaseName string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&databaseName).Error)
	require.Equal(t, "codego_release_test", databaseName, "release stress test requires its dedicated database")

	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		RegisterReleaseHook(nil)
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
		platformdb.UsingPostgreSQL = originalPostgreSQL
	})
	require.NoError(t, db.Exec("DROP SCHEMA IF EXISTS billing CASCADE").Error)
	require.NoError(t, db.Exec("DROP SCHEMA IF EXISTS marketplace CASCADE").Error)
	require.NoError(t, db.Exec("CREATE SCHEMA billing").Error)
	require.NoError(t, db.Exec("CREATE SCHEMA marketplace").Error)
	platformdb.DB = db
	platformdb.UsingSQLite = false
	platformdb.UsingPostgreSQL = true
	require.NoError(t, db.AutoMigrate(
		&billingschema.BillingAccount{},
		&billingschema.BillingBalanceSnapshot{},
		&billingschema.BillingLedgerEntry{},
		&billingschema.BillingReservation{},
		&billingschema.BillingSettlement{},
		&billingschema.BillingOutboxEvent{},
		&marketplaceschema.Settlement{},
	))

	t.Run("one hot owner", func(t *testing.T) {
		owners := []int{1001}
		seedReleaseStressRows(t, db, "hot", owners, 1000)
		maxByOwner, maxTotal, duration := runReleaseStressBatch(t, owners, 1000)
		require.Equal(t, 1, maxByOwner[owners[0]])
		require.Equal(t, 1, maxTotal)
		t.Logf("released 1000 settlements for one owner in %s", duration)
	})

	t.Run("eight independent owners", func(t *testing.T) {
		owners := []int{2001, 2002, 2003, 2004, 2005, 2006, 2007, 2008}
		seedReleaseStressRows(t, db, "parallel", owners, 1000)
		maxByOwner, maxTotal, duration := runReleaseStressBatch(t, owners, 1000)
		for _, owner := range owners {
			require.Equal(t, 1, maxByOwner[owner], "owner %d was released concurrently", owner)
		}
		require.Greater(t, maxTotal, 1, "independent owners should release concurrently")
		t.Logf("released 1000 settlements for eight owners in %s with concurrency %d", duration, maxTotal)
	})

	t.Run("sixteen independent owners", func(t *testing.T) {
		owners := []int{3001, 3002, 3003, 3004, 3005, 3006, 3007, 3008, 3009, 3010, 3011, 3012, 3013, 3014, 3015, 3016}
		seedReleaseStressRows(t, db, "parallel-12", owners, 1000)
		maxByOwner, maxTotal, duration := runReleaseStressBatch(t, owners, 1000)
		for _, owner := range owners {
			require.Equal(t, 1, maxByOwner[owner], "owner %d was released concurrently", owner)
		}
		require.Greater(t, maxTotal, 8, "independent owners should use the increased concurrency budget")
		require.LessOrEqual(t, maxTotal, releaseConcurrency)
		t.Logf("released 1000 settlements for sixteen owners in %s with concurrency %d", duration, maxTotal)
	})
}

func seedReleaseStressRows(t *testing.T, db *gorm.DB, prefix string, owners []int, total int) {
	t.Helper()
	perOwner := make(map[int]int64, len(owners))
	settlements := make([]marketplaceschema.Settlement, total)
	availableAt := time.Now().UTC().Add(-time.Hour)
	for index := range settlements {
		owner := owners[index%len(owners)]
		perOwner[owner]++
		id := fmt.Sprintf("%s-%04d", prefix, index)
		settlements[index] = marketplaceschema.Settlement{
			ID: id, RequestID: id, GroupID: "release-stress", OwnerUserID: owner,
			ConsumerUserID: 1, OwnerNetAmount: 1, Multiplier: 1, Status: statusPending,
			PendingAccountID: fmt.Sprintf("pending-%d", owner), AvailableAt: availableAt,
			CreatedAt: availableAt.Add(time.Duration(index) * time.Microsecond),
		}
	}
	for owner, amount := range perOwner {
		accountID := fmt.Sprintf("pending-%d", owner)
		require.NoError(t, db.Create(&billingschema.BillingAccount{
			AccountID: accountID, AccountType: "marketplace_owner_pending", OwnerType: "user",
			OwnerID: int64(owner), QuotaUnit: "quota", Status: billingschema.BillingAccountStatusActive,
		}).Error)
		require.NoError(t, db.Create(&billingschema.BillingBalanceSnapshot{
			AccountID: accountID, AvailableBalance: amount, GrantedTotal: amount,
		}).Error)
	}
	require.NoError(t, db.CreateInBatches(settlements, 500).Error)
}

func runReleaseStressBatch(t *testing.T, owners []int, total int) (map[int]int, int, time.Duration) {
	t.Helper()
	var lock sync.Mutex
	activeByOwner := make(map[int]int, len(owners))
	maxByOwner := make(map[int]int, len(owners))
	activeTotal := 0
	maxTotal := 0
	RegisterReleaseHook(func(_ *gorm.DB, owner int, _ int64, _, _ string) error {
		lock.Lock()
		activeByOwner[owner]++
		activeTotal++
		maxByOwner[owner] = max(maxByOwner[owner], activeByOwner[owner])
		maxTotal = max(maxTotal, activeTotal)
		lock.Unlock()
		time.Sleep(time.Millisecond)
		lock.Lock()
		activeByOwner[owner]--
		activeTotal--
		lock.Unlock()
		return nil
	})

	started := time.Now()
	selected, err := releaseDue(t.Context(), total, releaseConcurrency)
	duration := time.Since(started)
	require.NoError(t, err)
	require.Equal(t, total, selected)
	var released int64
	require.NoError(t, platformdb.DB.Model(&marketplaceschema.Settlement{}).
		Where("owner_user_id IN ? AND status = ?", owners, statusReleased).Count(&released).Error)
	require.EqualValues(t, total, released)
	return maxByOwner, maxTotal, duration
}
