package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDBTimestampPostgresAdvancesWithinTransaction(t *testing.T) {
	dsn := os.Getenv("TEST_LEDGER_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("real PostgreSQL fixture not configured: set TEST_LEDGER_POSTGRES_DSN")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	originalSQLite, originalPostgres := platformdb.UsingSQLite, platformdb.UsingPostgreSQL
	platformdb.UsingSQLite, platformdb.UsingPostgreSQL = false, true
	t.Cleanup(func() { platformdb.UsingSQLite, platformdb.UsingPostgreSQL = originalSQLite, originalPostgres })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var transactionStart int64
		if err := tx.Raw("SELECT EXTRACT(EPOCH FROM NOW())::bigint").Scan(&transactionStart).Error; err != nil {
			return err
		}
		if err := tx.Exec("SELECT pg_sleep(1.1)").Error; err != nil {
			return err
		}
		currentTime, err := GetDBTimestampTx(tx)
		require.NoError(t, err)
		assert.Greater(t, currentTime, transactionStart, "expiry checks must use current time after waiting, not the transaction start")
		return nil
	}))
	assert.Zero(t, sqlDB.Stats().WaitCount)
}

func TestDBTimestampUsesTransactionConnection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	originalSQLite, originalPostgres := platformdb.UsingSQLite, platformdb.UsingPostgreSQL
	platformdb.UsingSQLite, platformdb.UsingPostgreSQL = true, false
	t.Cleanup(func() { platformdb.UsingSQLite, platformdb.UsingPostgreSQL = originalSQLite, originalPostgres })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ts, err := GetDBTimestampTx(tx)
		require.NoError(t, err)
		assert.InDelta(t, time.Now().Unix(), ts, 2)
		return nil
	}))
	assert.Zero(t, sqlDB.Stats().WaitCount)
}

func TestDBTimestampTransactionReturnsQueryAndInvalidTimeErrors(t *testing.T) {
	_, err := GetDBTimestampTx(nil)
	require.Error(t, err)
	for _, queryFailure := range []bool{true, false} {
		t.Run(map[bool]string{true: "query_failure", false: "zero_timestamp"}[queryFailure], func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })
			originalSQLite, originalPostgres := platformdb.UsingSQLite, platformdb.UsingPostgreSQL
			platformdb.UsingSQLite, platformdb.UsingPostgreSQL = true, false
			t.Cleanup(func() { platformdb.UsingSQLite, platformdb.UsingPostgreSQL = originalSQLite, originalPostgres })
			queryErr := errors.New("database time query failed")
			require.NoError(t, db.Callback().Row().Before("gorm:row").Register("test:database_time_failure", func(tx *gorm.DB) {
				if queryFailure {
					tx.AddError(queryErr)
				} else {
					tx.Statement.SQL.Reset()
					tx.Statement.SQL.WriteString("SELECT 0")
				}
			}))
			err = db.Transaction(func(tx *gorm.DB) error {
				ts, err := GetDBTimestampTx(tx)
				assert.Zero(t, ts)
				return err
			})
			if queryFailure {
				require.ErrorIs(t, err, queryErr)
			} else {
				require.ErrorContains(t, err, "invalid database timestamp")
			}
		})
	}
}
