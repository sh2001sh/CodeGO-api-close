package projection

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	auditdomain "github.com/sh2001sh/new-api/internal/audit/domain"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestLogGroupFilterUsesKeywordMatching(t *testing.T) {
	db := setupLogQueryTestDB(t)
	logs := []auditschema.Log{
		{Id: 1, UserId: 7, Type: auditschema.LogTypeConsume, Group: "44-Codex Plus-0.075x", Quota: 12},
		{Id: 2, UserId: 7, Type: auditschema.LogTypeConsume, Group: "Codex Pro", Quota: 30},
		{Id: 3, UserId: 8, Type: auditschema.LogTypeConsume, Group: "44-Other User", Quota: 99},
	}
	require.NoError(t, db.Create(&logs).Error)

	items, total, err := ListUserLogs(7, auditdomain.LogListQuery{
		Group:    "44",
		PageSize: 20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	require.Equal(t, "44-Codex Plus-0.075x", items[0].Group)

	stat, err := SumUsedQuota(auditdomain.LogListQuery{Group: "44-Codex"})
	require.NoError(t, err)
	require.Equal(t, 12, stat.Quota)
}

func TestListUsedLogGroupsScopesUserOptions(t *testing.T) {
	db := setupLogQueryTestDB(t)
	logs := []auditschema.Log{
		{Id: 1, UserId: 7, Group: "44-Codex Plus-0.075x"},
		{Id: 2, UserId: 7, Group: "Codex Pro"},
		{Id: 3, UserId: 7, Group: "44-Codex Plus-0.075x"},
		{Id: 4, UserId: 8, Group: "Private Group"},
		{Id: 5, UserId: 7, Group: ""},
	}
	require.NoError(t, db.Create(&logs).Error)

	groups, err := ListUsedLogGroups(7)
	require.NoError(t, err)
	require.Equal(t, []string{"44-Codex Plus-0.075x", "Codex Pro"}, groups)

	adminGroups, err := ListUsedLogGroups(0)
	require.NoError(t, err)
	require.Contains(t, adminGroups, "Private Group")
}

func TestUsageLogQueriesDoNotMixUsersWithOverlappingNames(t *testing.T) {
	db := setupLogQueryTestDB(t)
	now := time.Now().Unix()
	logs := []auditschema.Log{
		{Id: 1, UserId: 7, Username: "user", Type: auditschema.LogTypeConsume, Quota: 12, CreatedAt: now},
		{Id: 2, UserId: 8, Username: "user-plus", Type: auditschema.LogTypeConsume, Quota: 30, CreatedAt: now},
	}
	require.NoError(t, db.Create(&logs).Error)

	items, total, err := ListAdminLogs(auditdomain.LogListQuery{
		UserID:   7,
		PageSize: 20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	require.Equal(t, 7, items[0].UserId)

	stat, err := SumUsedQuota(auditdomain.LogListQuery{UserID: 7})
	require.NoError(t, err)
	require.Equal(t, 12, stat.Quota)

	items, total, err = ListAdminLogs(auditdomain.LogListQuery{
		Username: "user",
		PageSize: 20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	require.Equal(t, 7, items[0].UserId)
}

func setupLogQueryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	originalDB, originalLogDB := platformdb.DB, platformdb.LogDB
	originalSQLite, originalPostgreSQL := platformdb.UsingSQLite, platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB, platformdb.LogDB = originalDB, originalLogDB
		platformdb.UsingSQLite, platformdb.UsingPostgreSQL = originalSQLite, originalPostgreSQL
	})
	platformdb.DB, platformdb.LogDB = db, db
	platformdb.UsingSQLite, platformdb.UsingPostgreSQL = true, false
	require.NoError(t, db.AutoMigrate(&auditschema.Log{}))
	return db
}
