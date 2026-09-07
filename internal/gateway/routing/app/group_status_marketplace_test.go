package app

import (
	"testing"

	"github.com/glebarez/sqlite"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMergeMarketplaceGroupModelsUsesDeclaredModels(t *testing.T) {
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

	summaries := map[string][]*GroupModelStatusSummary{group.InternalGroupName: {}}
	mergeMarketplaceGroupModels(summaries, []string{group.InternalGroupName})
	require.Len(t, summaries[group.InternalGroupName], 2)
	require.Equal(t, "gpt-5.2-codex", summaries[group.InternalGroupName][0].Model)
	displayNames := resolveGroupStatusDisplayNames([]string{group.InternalGroupName})
	require.Equal(t, group.SystemDisplayName, displayNames[group.InternalGroupName])
}

func TestResolveGroupStatusSourcesKeepsMarketplaceOfficialGroupsOfficial(t *testing.T) {
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

	sources := resolveGroupStatusSources([]string{"official-group", "market-group", "unlisted-group"})
	require.Equal(t, marketplacedomain.SourceTypeOfficial, sources["official-group"])
	require.Equal(t, marketplacedomain.SourceTypeMarketplaceUser, sources["market-group"])
	require.Equal(t, marketplacedomain.SourceTypeOfficial, sources["unlisted-group"])
}
