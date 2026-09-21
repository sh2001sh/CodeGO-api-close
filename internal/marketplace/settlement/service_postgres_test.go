package settlement

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresIncomeReclaimBatch(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		RegisterReclaimHook(nil)
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
		platformdb.UsingPostgreSQL = originalPostgreSQL
	})
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var databaseName string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&databaseName).Error)
	require.Equal(t, "codego_reclaim_test", databaseName, "PostgreSQL reclaim test requires its dedicated database")
	require.NoError(t, db.Exec("DROP SCHEMA IF EXISTS marketplace CASCADE").Error)
	require.NoError(t, db.Exec("CREATE SCHEMA marketplace").Error)
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS marketplace CASCADE").Error })
	platformdb.DB = db
	platformdb.UsingSQLite = false
	platformdb.UsingPostgreSQL = true
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Settlement{}, &marketplaceschema.IncomeReclaim{}))

	t.Run("exact amount can end in the middle of one settlement", func(t *testing.T) {
		reference := time.Now().UTC().Add(-time.Hour)
		items := []marketplaceschema.Settlement{
			postgresReclaimSettlement("partial-1", 10, 30, reference),
			postgresReclaimSettlement("partial-2", 10, 50, reference.Add(time.Second)),
			postgresReclaimSettlement("partial-3", 11, 70, reference.Add(2*time.Second)),
		}
		require.NoError(t, db.Create(&items).Error)
		transfers := make(map[int]int)
		RegisterReclaimHook(func(_ *gorm.DB, owner, _ int, amount int, _ string) error {
			transfers[owner] += amount
			return nil
		})
		task, err := CreateIncomeReclaimTask(ReleaseFilter{OwnerUserIDs: []int{10, 11}, MaxAmount: 100, OperationID: "postgres-partial"})
		require.NoError(t, err)
		task, err = ProcessIncomeReclaimTask(task.ID)
		require.NoError(t, err)
		require.Equal(t, reclaimTaskCompleted, task.Status)
		require.Equal(t, 3, task.Count)
		require.EqualValues(t, 100, task.Amount)
		require.Equal(t, map[int]int{10: 80, 11: 20}, transfers)

		var updated []marketplaceschema.Settlement
		require.NoError(t, db.Where("request_id LIKE ?", "partial-%").Order("created_at, id").Find(&updated).Error)
		require.Equal(t, []int64{30, 50, 20}, []int64{updated[0].ReclaimedAmount, updated[1].ReclaimedAmount, updated[2].ReclaimedAmount})
		require.Equal(t, []string{statusReclaimed, statusReclaimed, statusReleased}, []string{updated[0].Status, updated[1].Status, updated[2].Status})

		repeated, err := ProcessIncomeReclaimTask(task.ID)
		require.NoError(t, err)
		require.Equal(t, task.Count, repeated.Count)
		require.Equal(t, task.Amount, repeated.Amount)
		require.Equal(t, map[int]int{10: 80, 11: 20}, transfers)
	})

	t.Run("hook failure rolls back settlement updates", func(t *testing.T) {
		item := postgresReclaimSettlement("rollback", 12, 40, time.Now().UTC())
		require.NoError(t, db.Create(&item).Error)
		RegisterReclaimHook(func(_ *gorm.DB, _, _ int, _ int, _ string) error {
			return errors.New("forced transfer failure")
		})
		task, err := CreateIncomeReclaimTask(ReleaseFilter{OwnerUserIDs: []int{12}, OperationID: "postgres-rollback"})
		require.NoError(t, err)
		_, err = ProcessIncomeReclaimTask(task.ID)
		require.ErrorContains(t, err, "forced transfer failure")
		require.NoError(t, db.First(&item, "request_id = ?", "rollback").Error)
		require.Zero(t, item.ReclaimedAmount)
		require.Equal(t, statusReleased, item.Status)
	})

	t.Run("full reclaim continues after a complete batch", func(t *testing.T) {
		items := make([]marketplaceschema.Settlement, reclaimBatchSize+1)
		reference := time.Now().UTC().Add(time.Hour)
		for index := range items {
			items[index] = postgresReclaimSettlement(fmt.Sprintf("large-%05d", index), 13, 1, reference)
		}
		require.NoError(t, db.CreateInBatches(items, 500).Error)
		transferred := 0
		RegisterReclaimHook(func(_ *gorm.DB, owner, _ int, amount int, _ string) error {
			require.Equal(t, 13, owner)
			transferred += amount
			return nil
		})
		task, err := CreateIncomeReclaimTask(ReleaseFilter{OwnerUserIDs: []int{13}, OperationID: "postgres-large"})
		require.NoError(t, err)
		task, err = ProcessIncomeReclaimTask(task.ID)
		require.NoError(t, err)
		require.Equal(t, reclaimTaskRunning, task.Status)
		require.Equal(t, reclaimBatchSize, task.Count)
		task, err = ProcessIncomeReclaimTask(task.ID)
		require.NoError(t, err)
		require.Equal(t, reclaimTaskCompleted, task.Status)
		require.Equal(t, reclaimBatchSize+1, task.Count)
		require.Equal(t, reclaimBatchSize+1, transferred)
	})
}

func postgresReclaimSettlement(requestID string, owner int, amount int64, createdAt time.Time) marketplaceschema.Settlement {
	return marketplaceschema.Settlement{
		RequestID: requestID, GroupID: "postgres-test", OwnerUserID: owner,
		OwnerNetAmount: amount, Status: statusReleased, CreatedAt: createdAt,
	}
}
