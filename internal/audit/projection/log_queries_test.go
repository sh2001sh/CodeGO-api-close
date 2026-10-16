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

func TestUserLogCountBoundsInputAndPreservesPageRows(t *testing.T) {
	db := setupLogQueryTestDB(t)
	logs := make([]auditschema.Log, logSearchCountLimit+25)
	for i := range logs {
		logs[i] = auditschema.Log{UserId: 7, Type: auditschema.LogTypeConsume, Group: "test"}
	}
	require.NoError(t, db.CreateInBatches(logs, 200).Error)
	items, total, err := ListUserLogs(7, auditdomain.LogListQuery{PageSize: 20})
	require.NoError(t, err)
	require.EqualValues(t, logSearchCountLimit, total)
	require.Len(t, items, 20)
	require.Equal(t, "test", items[0].Group, "count projection must not leak into the row query")
	items, total, err = ListUserLogs(8, auditdomain.LogListQuery{PageSize: 20})
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, items)
}

func TestAdminLogCountBoundsInputAndPreservesPageRows(t *testing.T) {
	db := setupLogQueryTestDB(t)
	logs := make([]auditschema.Log, logSearchCountLimit+25)
	for i := range logs {
		logs[i] = auditschema.Log{UserId: i%3 + 1, Type: auditschema.LogTypeConsume, Group: "admin-test"}
	}
	require.NoError(t, db.CreateInBatches(logs, 200).Error)
	items, total, err := ListAdminLogs(auditdomain.LogListQuery{PageSize: 20})
	require.NoError(t, err)
	require.EqualValues(t, logSearchCountLimit, total)
	require.Len(t, items, 20)
	require.Equal(t, "admin-test", items[0].Group)
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

func TestUsageStatKeepsRangeIndependentFromLiveMinuteAndFilters(t *testing.T) {
	db := setupLogQueryTestDB(t)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&[]auditschema.Log{
		{UserId: 7, Username: "exact", Type: auditschema.LogTypeConsume, CreatedAt: now - 3600, Quota: 12, PromptTokens: 100, CompletionTokens: 10, TokenName: "key%one", ModelName: "model_one", ChannelId: 3, Group: "group-one"},
		{UserId: 7, Username: "exact", Type: auditschema.LogTypeConsume, CreatedAt: now - 10, Quota: 30, PromptTokens: 20, CompletionTokens: 3, TokenName: "key%one", ModelName: "model_one", ChannelId: 3, Group: "group-one"},
		{UserId: 8, Username: "exact-plus", Type: auditschema.LogTypeConsume, CreatedAt: now - 10, Quota: 999, PromptTokens: 999, TokenName: "key%one", ModelName: "model_one", ChannelId: 3, Group: "group-one"},
		{UserId: 7, Username: "exact", Type: auditschema.LogTypeRefund, CreatedAt: now - 10, Quota: 999, PromptTokens: 999, TokenName: "key%one", ModelName: "model_one", ChannelId: 3, Group: "group-one"},
		{UserId: 7, Username: "exact", Type: auditschema.LogTypeConsume, CreatedAt: now - 10, Quota: 999, PromptTokens: 999, TokenName: "keyXone", ModelName: "model_one", ChannelId: 3, Group: "group-one"},
	}).Error)
	query := auditdomain.LogListQuery{UserID: 7, Username: "exact", TokenName: "%", ModelName: "_", Channel: 3, Group: "one", StartTimestamp: now - 7200, EndTimestamp: now - 120}
	stat, err := SumUsedQuota(query)
	require.NoError(t, err)
	require.Equal(t, auditschema.Stat{Quota: 12, Rpm: 1, Tpm: 23}, stat)

	query.UserID = 8
	query.Username = "exact-plus"
	stat, err = SumUsedQuota(query)
	require.NoError(t, err)
	require.Equal(t, auditschema.Stat{Rpm: 1, Tpm: 999}, stat, "user scope and exact username must be part of the in-flight identity")

	query.UserID = 999
	stat, err = SumUsedQuota(query)
	require.NoError(t, err)
	require.Equal(t, auditschema.Stat{}, stat)
}

func TestUsageStatDoesNotRetainCompletedResultsOrErrors(t *testing.T) {
	db := setupLogQueryTestDB(t)
	query := auditdomain.LogListQuery{UserID: 7}
	stat, err := SumUsedQuota(query)
	require.NoError(t, err)
	require.Zero(t, stat.Quota)
	require.NoError(t, db.Create(&auditschema.Log{UserId: 7, Type: auditschema.LogTypeConsume, CreatedAt: time.Now().Unix(), Quota: 11, PromptTokens: 2, CompletionTokens: 3}).Error)
	stat, err = SumUsedQuota(query)
	require.NoError(t, err)
	require.Equal(t, auditschema.Stat{Quota: 11, Rpm: 1, Tpm: 5}, stat)

	require.NoError(t, db.Migrator().RenameTable(&auditschema.Log{}, "temporarily_unavailable_logs"))
	_, err = SumUsedQuota(query)
	require.Error(t, err)
	require.NoError(t, db.Migrator().RenameTable("temporarily_unavailable_logs", "logs"))
	stat, err = SumUsedQuota(query)
	require.NoError(t, err)
	require.Equal(t, 11, stat.Quota, "a failed in-flight read must not poison later requests")
}

func TestConcurrentUsageStatsKeepDifferentUsersSeparate(t *testing.T) {
	db := setupLogQueryTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&[]auditschema.Log{
		{UserId: 7, Type: auditschema.LogTypeConsume, CreatedAt: now, Quota: 11, PromptTokens: 2, CompletionTokens: 3},
		{UserId: 8, Type: auditschema.LogTypeConsume, CreatedAt: now, Quota: 99, PromptTokens: 20, CompletionTokens: 30},
	}).Error)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("test:hold-user-stat", func(tx *gorm.DB) {
		entered <- struct{}{}
		<-release
	}))
	t.Cleanup(func() { _ = db.Callback().Row().Remove("test:hold-user-stat") })
	type result struct {
		user int
		stat auditschema.Stat
		err  error
	}
	results := make(chan result, 2)
	for _, userID := range []int{7, 8} {
		go func(id int) {
			stat, err := SumUsedQuota(auditdomain.LogListQuery{UserID: id})
			results <- result{id, stat, err}
		}(userID)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("different users incorrectly shared one in-flight query")
		}
	}
	close(release)
	released = true
	for i := 0; i < 2; i++ {
		got := <-results
		require.NoError(t, got.err)
		if got.user == 7 {
			require.Equal(t, auditschema.Stat{Quota: 11, Rpm: 1, Tpm: 5}, got.stat)
		} else {
			require.Equal(t, auditschema.Stat{Quota: 99, Rpm: 1, Tpm: 50}, got.stat)
		}
	}
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
