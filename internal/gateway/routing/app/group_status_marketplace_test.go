package app

import (
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGroupStatusUsesDeclaredModelsAndMarketplaceDisplayName(t *testing.T) {
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	platformdb.UsingSQLite = true
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}))

	channel := marketplaceschema.Channel{
		ID: "channel-status", OwnerUserID: 4, ProviderType: "codex",
		BaseURLCiphertext: "url", CredentialCiphertext: "key",
		DeclaredModels: `["gpt-5.2-codex","gpt-5.1-codex"]`, Status: "active",
	}
	group := marketplaceschema.Group{
		ID: "group-status", ChannelID: channel.ID, OwnerUserID: 4,
		PublicSlug: "group-status", SystemDisplayName: "Codex Plus",
		InternalGroupName: "Codex-Plus-ae381d", SourceType: "marketplace_user",
		CreditPoolPolicy: "marketplace_universal_only", Multiplier: 1,
		LifecycleStatus: "active", VerificationStatus: "passed", Visibility: "public",
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)

	items, err := buildGroupStatusForGroups(nil, []string{group.InternalGroupName})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Len(t, items[0].Models, 2)
	require.Equal(t, "gpt-5.1-codex", items[0].Models[0].Model)
	require.Equal(t, "gpt-5.2-codex", items[0].Models[1].Model)
	require.Equal(t, group.SystemDisplayName, items[0].DisplayName)
	require.Equal(t, group.ID, items[0].GroupID)
	require.Equal(t, marketplacedomain.SourceTypeMarketplaceUser, items[0].SourceType)
}

func TestGroupStatusKeepsMarketplaceOfficialGroupsOfficial(t *testing.T) {
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	platformdb.UsingSQLite = true
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &marketplaceschema.Channel{}))

	require.NoError(t, db.Create(&marketplaceschema.Group{
		ID: "official-status", ChannelID: "official-channel", OwnerUserID: 1,
		PublicSlug: "official-status", SystemDisplayName: "官方渠道",
		InternalGroupName: "official-group", SourceType: marketplacedomain.SourceTypeOfficial,
		CreditPoolPolicy: "marketplace_universal_only", Multiplier: 1,
		LifecycleStatus: "active", VerificationStatus: "passed", Visibility: "public",
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Group{
		ID: "market-status", ChannelID: "market-channel", OwnerUserID: 2,
		PublicSlug: "market-status", SystemDisplayName: "第三方渠道",
		InternalGroupName: "market-group", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: "marketplace_universal_only", Multiplier: 1,
		LifecycleStatus: "active", VerificationStatus: "passed", Visibility: "public",
	}).Error)

	items, err := buildGroupStatusForGroups(nil, []string{"official-group", "market-group", "unlisted-group"})
	require.NoError(t, err)
	require.Len(t, items, 3)
	byName := make(map[string]UserGroupStatusItem, len(items))
	for _, item := range items {
		byName[item.Group] = item
	}
	require.Equal(t, marketplacedomain.SourceTypeOfficial, byName["official-group"].SourceType)
	require.Equal(t, "official:official-group", byName["official-group"].GroupID)
	require.Equal(t, "官方渠道", byName["official-group"].DisplayName)
	require.Equal(t, marketplacedomain.SourceTypeMarketplaceUser, byName["market-group"].SourceType)
	require.Equal(t, "market-status", byName["market-group"].GroupID)
	require.Equal(t, "第三方渠道", byName["market-group"].DisplayName)
	require.Equal(t, marketplacedomain.SourceTypeOfficial, byName["unlisted-group"].SourceType)
	require.Equal(t, "official:unlisted-group", byName["unlisted-group"].GroupID)
	require.Equal(t, "unlisted-group", byName["unlisted-group"].DisplayName)
}

func TestResolveVisibleGroupStatusGroupsIncludesPublicMarketplaceGroupsForAnonymousUsers(t *testing.T) {
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	platformdb.UsingSQLite = true
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}))

	require.NoError(t, db.Create(&marketplaceschema.Group{
		ID: "public-market", ChannelID: "market-channel", OwnerUserID: 2,
		PublicSlug: "public-market", SystemDisplayName: "公开第三方分组",
		InternalGroupName: "public-market-group", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: "marketplace_universal_only", Multiplier: 1,
		LifecycleStatus: marketplacedomain.LifecycleActive, VerificationStatus: "passed", Visibility: marketplacedomain.VisibilityPublic,
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Group{
		ID: "private-market", ChannelID: "private-channel", OwnerUserID: 3,
		PublicSlug: "private-market", SystemDisplayName: "私有第三方分组",
		InternalGroupName: "private-market-group", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: "marketplace_universal_only", Multiplier: 1,
		LifecycleStatus: marketplacedomain.LifecycleActive, VerificationStatus: "passed", Visibility: "private",
	}).Error)

	groups, err := resolveVisibleGroupStatusGroups(0, false, nil)

	require.NoError(t, err)
	require.Contains(t, groups, "public-market-group")
	require.NotContains(t, groups, "private-market-group")
}

func TestGroupStatusUsesLatestSeriesWithoutRedundantSummary(t *testing.T) {
	originalDB, originalSQLite := platformdb.DB, platformdb.UsingSQLite
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingSQLite = originalDB, originalSQLite
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB, platformdb.UsingSQLite = db, true
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &marketplaceschema.Channel{}))
	require.NoError(t, db.Exec(`CREATE TABLE perf_metrics (
		id INTEGER PRIMARY KEY, model_name TEXT, "group" TEXT, bucket_ts BIGINT,
		request_count BIGINT DEFAULT 0, success_count BIGINT DEFAULT 0, total_latency_ms BIGINT DEFAULT 0,
		ttft_sum_ms BIGINT DEFAULT 0, ttft_count BIGINT DEFAULT 0, output_tokens BIGINT DEFAULT 0,
		generation_ms BIGINT DEFAULT 0, input_tokens BIGINT DEFAULT 0,
		cache_read_tokens BIGINT DEFAULT 0, cache_write_tokens BIGINT DEFAULT 0)`).Error)
	bucketSeconds := auditprojection.PerfMetricsBucketSeconds()
	now := time.Now().Unix()
	require.NoError(t, db.Exec(`INSERT INTO perf_metrics (model_name, "group", bucket_ts, request_count, success_count)
		VALUES (?, ?, ?, ?, ?)`, "active", "alpha", now-now%bucketSeconds, 4, 3).Error)
	pricing := []gatewaydomain.Pricing{
		{ModelName: "active", EnableGroup: []string{"alpha"}},
		{ModelName: "idle-a", EnableGroup: []string{"alpha"}},
		{ModelName: "idle-b", EnableGroup: []string{"alpha"}},
		{ModelName: "private-model", EnableGroup: []string{"beta"}},
	}
	queries, summaries := 0, 0
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("count_status_queries", func(tx *gorm.DB) {
		queries++
		if strings.Contains(strings.ToLower(tx.Statement.SQL.String()), "sum(request_count)") {
			summaries++
		}
	}))
	items, err := buildGroupStatusForGroups(pricing, []string{"alpha"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Len(t, items[0].Models, 3, "other authorized sets must not leak into this result")
	require.Equal(t, gatewaydomain.RequestHealthUnstable, items[0].Status)
	require.EqualValues(t, 4, items[0].RequestCount)
	require.Equal(t, 75.0, *items[0].SuccessRate)
	require.Equal(t, 2, summaries, "only 24-hour group and model cache-hit aggregates are needed")
	require.Equal(t, items[0].Models[1].Series, items[0].Models[2].Series)
	require.Equal(t, gatewaydomain.RequestHealthUnknown, items[0].Models[1].Status)
	// A fresh read must see the next health update; no TTL masks failures.
	require.NoError(t, db.Exec("UPDATE perf_metrics SET success_count = 0").Error)
	items, err = buildGroupStatusForGroups(pricing, []string{"alpha"})
	require.NoError(t, err)
	require.Equal(t, gatewaydomain.RequestHealthFailed, items[0].Status)
	require.Equal(t, 0.0, *items[0].SuccessRate)
	queriesBefore := queries
	items, err = buildGroupStatusForGroups(nil, nil)
	require.NoError(t, err)
	require.Empty(t, items)
	require.Equal(t, queriesBefore, queries, "empty visibility must not trigger an unfiltered analytics query")
}
