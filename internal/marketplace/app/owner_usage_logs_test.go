package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestListOwnerUsageLogsScopesAndSanitizesChannelCalls(t *testing.T) {
	db, logDB := openOwnerUsageLogTestDB(t)
	ownerInternalID := 101
	foreignInternalID := 202
	require.NoError(t, db.Create([]marketplaceschema.Channel{
		{ID: "owner-channel", OwnerUserID: 10, InternalChannelID: &ownerInternalID, Status: "active", ProviderType: "openai"},
		{ID: "foreign-channel", OwnerUserID: 11, InternalChannelID: &foreignInternalID, Status: "active", ProviderType: "openai"},
	}).Error)
	require.NoError(t, db.Create([]marketplaceschema.Group{
		{ID: "owner-group", ChannelID: "owner-channel", OwnerUserID: 10, SystemDisplayName: "Codex-Plus-owner", InternalGroupName: "Codex-Plus-owner", PublicSlug: "owner", SourceType: "marketplace_user", CreditPoolPolicy: "universal", LifecycleStatus: "active", VerificationStatus: "passed", Visibility: "public", Multiplier: 1},
		{ID: "foreign-group", ChannelID: "foreign-channel", OwnerUserID: 11, SystemDisplayName: "Codex-Plus-foreign", InternalGroupName: "Codex-Plus-foreign", PublicSlug: "foreign", SourceType: "marketplace_user", CreditPoolPolicy: "universal", LifecycleStatus: "active", VerificationStatus: "passed", Visibility: "public", Multiplier: 1},
	}).Error)
	require.NoError(t, db.Create([]identityschema.User{
		{Id: 300, ExternalId: "A2B3C4", Username: "consumer-300", Password: "password", AffCode: "AFF300"},
		{Id: 301, ExternalId: "D5E6F7", Username: "consumer-301", Password: "password", AffCode: "AFF301"},
	}).Error)
	now := time.Now().Unix()
	require.NoError(t, logDB.Create([]auditschema.Log{
		{UserId: 300, CreatedAt: now, Type: auditschema.LogTypeConsume, ChannelId: ownerInternalID, RequestId: "owner-success", ModelName: "gpt-5", Quota: 100, Username: "secret-user", TokenName: "secret-token", Ip: "127.0.0.1", Content: "secret-content", Other: "secret-other"},
		{UserId: 301, CreatedAt: now - 1, Type: auditschema.LogTypeError, ChannelId: ownerInternalID, RequestId: "owner-error", ModelName: "claude-sonnet", Content: "sensitive error"},
		{UserId: 999, CreatedAt: now - 2, Type: auditschema.LogTypeConsume, ChannelId: foreignInternalID, RequestId: "foreign-success", ModelName: "gpt-5"},
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{
		RequestID: "owner-success", GroupID: "owner-group", OwnerUserID: 10, ConsumerUserID: 300,
		ConsumerAmount: 100, PlatformCommission: 5, OwnerNetAmount: 95, Multiplier: 1,
		Status: "pending", AvailableAt: time.Now().Add(time.Hour),
	}).Error)

	result, err := ListOwnerUsageLogs(10, OwnerUsageLogQuery{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.EqualValues(t, 2, result.Total)
	require.EqualValues(t, 2, result.Summary.RequestCount)
	require.EqualValues(t, 1, result.Summary.SuccessCount)
	require.EqualValues(t, 1, result.Summary.FailedCount)
	require.Equal(t, int64(100), result.Summary.ConsumerAmount)
	require.Equal(t, int64(95), result.Summary.OwnerIncome)
	require.Len(t, result.Items, 2)
	itemsByRequestID := make(map[string]OwnerUsageLogItem, len(result.Items))
	for _, item := range result.Items {
		itemsByRequestID[item.RequestID] = item
	}
	success := itemsByRequestID["owner-success"]
	require.Equal(t, "A2B3C4", success.UserID)
	require.Equal(t, int64(95), success.OwnerIncome)
	require.Equal(t, "pending", success.IncomeStatus)
	failed := itemsByRequestID["owner-error"]
	require.Equal(t, "D5E6F7", failed.UserID)
	require.Zero(t, failed.OwnerIncome)
	require.Equal(t, "failed", failed.Status)
	payload, err := json.Marshal(result.Items)
	require.NoError(t, err)
	for _, sensitiveKey := range []string{
		`"user_id":300`,
		`"user_id":301`,
		`"username":`,
		`"token_name":`,
		`"ip":`,
		`"content":`,
		`"other":`,
		`"upstream_request_id":`,
	} {
		require.NotContains(t, string(payload), sensitiveKey)
	}
}

func TestListOwnerUsageLogsFiltersSingleOwnedChannel(t *testing.T) {
	db, logDB := openOwnerUsageLogTestDB(t)
	firstInternalID, secondInternalID := 101, 102
	require.NoError(t, db.Create([]marketplaceschema.Channel{
		{ID: "first", OwnerUserID: 10, InternalChannelID: &firstInternalID, Status: "active", ProviderType: "openai"},
		{ID: "second", OwnerUserID: 10, InternalChannelID: &secondInternalID, Status: "active", ProviderType: "openai"},
	}).Error)
	require.NoError(t, db.Create([]marketplaceschema.Group{
		{ID: "first-group", ChannelID: "first", OwnerUserID: 10, SystemDisplayName: "First", InternalGroupName: "First", PublicSlug: "first", SourceType: "marketplace_user", CreditPoolPolicy: "universal", LifecycleStatus: "active", VerificationStatus: "passed", Visibility: "public", Multiplier: 1},
		{ID: "second-group", ChannelID: "second", OwnerUserID: 10, SystemDisplayName: "Second", InternalGroupName: "Second", PublicSlug: "second", SourceType: "marketplace_user", CreditPoolPolicy: "universal", LifecycleStatus: "active", VerificationStatus: "passed", Visibility: "public", Multiplier: 1},
	}).Error)
	require.NoError(t, db.Create([]identityschema.User{
		{Id: 201, ExternalId: "G8H9J2", Username: "consumer-201", Password: "password", AffCode: "AFF201"},
		{Id: 202, ExternalId: "K3L4M5", Username: "consumer-202", Password: "password", AffCode: "AFF202"},
	}).Error)
	require.NoError(t, logDB.Create([]auditschema.Log{
		{UserId: 201, Type: auditschema.LogTypeConsume, ChannelId: firstInternalID, RequestId: "first-request"},
		{UserId: 202, Type: auditschema.LogTypeConsume, ChannelId: secondInternalID, RequestId: "second-request"},
	}).Error)

	result, err := ListOwnerUsageLogs(10, OwnerUsageLogQuery{ChannelID: "second"})
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Total)
	require.Len(t, result.Items, 1)
	require.Equal(t, "second", result.Items[0].ChannelID)
	require.Equal(t, "K3L4M5", result.Items[0].UserID)
}

func TestListOwnerUsageLogsRejectsForeignChannelFilter(t *testing.T) {
	openOwnerUsageLogTestDB(t)
	_, err := ListOwnerUsageLogs(10, OwnerUsageLogQuery{ChannelID: "foreign-channel"})
	require.EqualError(t, err, "渠道不存在或无权访问")
}

func openOwnerUsageLogTestDB(t *testing.T) (*gorm.DB, *gorm.DB) {
	t.Helper()
	originalDB, originalLogDB := platformdb.DB, platformdb.LogDB
	originalSQLite, originalPostgreSQL := platformdb.UsingSQLite, platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB, platformdb.LogDB = originalDB, originalLogDB
		platformdb.UsingSQLite, platformdb.UsingPostgreSQL = originalSQLite, originalPostgreSQL
	})
	platformdb.UsingSQLite, platformdb.UsingPostgreSQL = true, false
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	logDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB, platformdb.LogDB = db, logDB
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &marketplaceschema.Channel{}, &marketplaceschema.Group{}, &marketplaceschema.Settlement{}))
	require.NoError(t, logDB.AutoMigrate(&auditschema.Log{}))
	return db, logDB
}
