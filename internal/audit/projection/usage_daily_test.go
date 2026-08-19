package projection

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRebuildUsageDailyIncrementallyIncludesLateHistoricalLogs(t *testing.T) {
	db := setupUsageDailyTestDB(t)
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	logs := []auditschema.Log{
		{Id: 1, UserId: 7, ChannelId: 6, Type: auditschema.LogTypeConsume, CreatedAt: now.AddDate(0, 0, -10).Unix(), PromptTokens: 10, CompletionTokens: 2, Quota: 12},
		{Id: 2, UserId: 8, ChannelId: 9, Type: auditschema.LogTypeConsume, CreatedAt: now.AddDate(0, 0, -1).Unix(), PromptTokens: 20, CompletionTokens: 3, Quota: 23},
	}
	require.NoError(t, db.Create(&logs).Error)
	require.NoError(t, rebuildUsageDailyAt(context.Background(), now))

	oldDay := now.AddDate(0, 0, -10).Format(time.DateOnly)
	var initial UserUsageDaily
	require.NoError(t, db.Where("day = ? AND user_id = ?", oldDay, 7).First(&initial).Error)
	require.Equal(t, int64(1), initial.RequestCount)

	late := auditschema.Log{Id: 3, UserId: 7, ChannelId: 6, Type: auditschema.LogTypeConsume, CreatedAt: now.AddDate(0, 0, -10).Add(time.Hour).Unix(), PromptTokens: 30, CompletionTokens: 4, Quota: 34}
	today := auditschema.Log{Id: 4, UserId: 7, ChannelId: 9, Type: auditschema.LogTypeConsume, CreatedAt: now.Unix(), PromptTokens: 40, CompletionTokens: 5, Quota: 45}
	require.NoError(t, db.Create(&[]auditschema.Log{late, today}).Error)
	require.NoError(t, rebuildUsageDailyAt(context.Background(), now))

	var rebuilt UserUsageDaily
	require.NoError(t, db.Where("day = ? AND user_id = ?", oldDay, 7).First(&rebuilt).Error)
	require.Equal(t, int64(2), rebuilt.RequestCount)
	require.Equal(t, int64(40), rebuilt.PromptTokens)
	require.Equal(t, int64(6), rebuilt.CompletionTokens)
	require.Equal(t, int64(46), rebuilt.Quota)

	var channel ChannelUsageDaily
	require.NoError(t, db.Where("day = ? AND channel_id = ?", oldDay, 6).First(&channel).Error)
	require.Equal(t, int64(2), channel.RequestCount)
	var cursor UsageDailyCursor
	require.NoError(t, db.First(&cursor, "name = ?", usageDailyCursorName).Error)
	require.Equal(t, int64(4), cursor.LastLogID)
}

func TestRebuildUsageDailyInitializesCursorWithoutRescanningExistingHistory(t *testing.T) {
	db := setupUsageDailyTestDB(t)
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	oldDay := now.AddDate(0, 0, -30).Format(time.DateOnly)
	require.NoError(t, db.Create(&UserUsageDaily{Day: oldDay, UserID: 1, RequestCount: 99}).Error)
	require.NoError(t, db.Create(&ChannelUsageDaily{Day: oldDay, ChannelID: 2, RequestCount: 99}).Error)
	require.NoError(t, db.Create(&auditschema.Log{Id: 10, UserId: 1, ChannelId: 2, Type: auditschema.LogTypeConsume, CreatedAt: now.AddDate(0, 0, -30).Unix()}).Error)

	require.NoError(t, rebuildUsageDailyAt(context.Background(), now))
	var user UserUsageDaily
	require.NoError(t, db.Where("day = ? AND user_id = ?", oldDay, 1).First(&user).Error)
	require.Equal(t, int64(99), user.RequestCount)
	var cursor UsageDailyCursor
	require.NoError(t, db.First(&cursor, "name = ?", usageDailyCursorName).Error)
	require.Equal(t, int64(10), cursor.LastLogID)
}

func TestUsageDayExpressionSupportsAllDatabaseDialects(t *testing.T) {
	require.Equal(t, "date(created_at, 'unixepoch')", usageDayExpression("sqlite"))
	require.Contains(t, usageDayExpression("postgres"), "AT TIME ZONE 'UTC'")
	require.Contains(t, usageDayExpression("mysql"), "TIMESTAMPADD")
}

func setupUsageDailyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	originalDB, originalLogDB := platformdb.DB, platformdb.LogDB
	originalSQLite, originalPostgreSQL, originalMySQL := platformdb.UsingSQLite, platformdb.UsingPostgreSQL, platformdb.UsingMySQL
	t.Cleanup(func() {
		platformdb.DB, platformdb.LogDB = originalDB, originalLogDB
		platformdb.UsingSQLite, platformdb.UsingPostgreSQL, platformdb.UsingMySQL = originalSQLite, originalPostgreSQL, originalMySQL
	})
	platformdb.DB, platformdb.LogDB = db, db
	platformdb.UsingSQLite, platformdb.UsingPostgreSQL, platformdb.UsingMySQL = true, false, false
	require.NoError(t, db.AutoMigrate(&auditschema.Log{}, &UserUsageDaily{}, &ChannelUsageDaily{}, &UsageDailyCursor{}))
	return db
}
