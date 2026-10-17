package app

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	gatewaygroups "github.com/sh2001sh/new-api/internal/gateway/groupsettings"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupModelRankingsTest(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &marketplaceschema.Channel{}, &gatewayschema.Channel{}, &gatewayschema.Model{}))
	originalDB, originalGroups := platformdb.DB, gatewaygroups.UserUsableGroups2JSONString()
	originalMySQL, originalPostgres := platformdb.UsingMySQL, platformdb.UsingPostgreSQL
	platformdb.DB, platformdb.UsingMySQL, platformdb.UsingPostgreSQL = db, false, false
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"default":"Public", "private-market":"must not bypass visibility"}`))
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingMySQL, platformdb.UsingPostgreSQL = originalDB, originalMySQL, originalPostgres
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(originalGroups))
	})
	return db
}

func TestPublicModelRankingScopePreservesHistoryWithoutPrivateModels(t *testing.T) {
	db := setupModelRankingsTest(t)
	for _, channel := range []gatewayschema.Channel{
		{Id: 1, Models: "shared,official-only,hidden,shared", Group: "default", ChannelScope: "official", Status: 1},
		{Id: 2, Models: "paused-official", Group: "default", ChannelScope: "official", Status: 2},
		{Id: 3, Models: "secret-official", Group: "vip", ChannelScope: "official", Status: 1},
		{Id: 4, Models: "undeclared", Group: "default", ChannelScope: "external", Status: 1},
		{Id: 5, Models: "private-bypass", Group: "private-market", ChannelScope: "official", Status: 1},
	} {
		require.NoError(t, db.Create(&channel).Error)
	}
	hidden := gatewayschema.Model{ModelName: "hidden", Status: 1}
	require.NoError(t, db.Create(&hidden).Error)
	require.NoError(t, db.Model(&hidden).Update("status", 0).Error)
	for _, test := range []struct {
		id, visibility, status, verification, moderation, models string
		deleted                                                  bool
	}{
		{"third-party", "public", "active", "passed", "", `["shared","third-only","hidden"]`, false},
		{"paused", "public", "disabled", "passed", "", `["paused-model"]`, false},
		{"deleted", "public", "disabled", "passed", "", `["deleted-model"]`, true},
		{"private-market", "private", "active", "passed", "", `["secret-market"]`, false},
		{"unlisted", "unlisted", "active", "passed", "", `["unlisted-model"]`, false},
		{"draft", "public", "draft", "queued", "", `["draft-model"]`, false},
		{"blocked", "public", "active", "passed", "failed", `["blocked-model"]`, false},
	} {
		channel := marketplaceschema.Channel{ID: test.id, OwnerUserID: 10, ProviderType: "openai", DeclaredModels: test.models, Status: test.status}
		group := marketplaceschema.Group{ID: test.id, ChannelID: test.id, OwnerUserID: 10, PublicSlug: test.id, InternalGroupName: test.id, SourceType: marketplacedomain.SourceTypeMarketplaceUser,
			Visibility: test.visibility, LifecycleStatus: test.status, VerificationStatus: test.verification, ModerationStatus: test.moderation}
		if test.status != "draft" {
			now := time.Now().UTC()
			group.PublishedAt = &now
		}
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&group).Error)
		if test.deleted {
			require.NoError(t, db.Delete(&channel).Error)
			require.NoError(t, db.Delete(&group).Error)
		}
	}
	scope, err := publicModelRankingScope()
	require.NoError(t, err)
	require.Equal(t, map[string][]string{
		"default":     {"official-only", "paused-official", "shared"},
		"third-party": {"shared", "third-only"},
		"paused":      {"paused-model"}, "deleted": {"deleted-model"},
	}, scope)
	// A privacy change must apply immediately to every subsequent ranking read.
	require.NoError(t, db.Model(&marketplaceschema.Group{}).Where("id = ?", "third-party").Update("visibility", "private").Error)
	scope, err = publicModelRankingScope()
	require.NoError(t, err)
	require.NotContains(t, scope, "third-party")
}

func TestPublicModelRankingScopeFailsClosedOnBrokenMetadata(t *testing.T) {
	db := setupModelRankingsTest(t)
	require.NoError(t, db.Create(&marketplaceschema.Channel{ID: "bad", DeclaredModels: `invalid json`}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Group{ID: "bad", ChannelID: "bad", PublicSlug: "bad", InternalGroupName: "bad", Visibility: "public", LifecycleStatus: "active", VerificationStatus: "passed"}).Error)
	_, err := publicModelRankingScope()
	require.Error(t, err)
	require.NoError(t, db.Migrator().DropTable(&marketplaceschema.Group{}))
	_, err = BuildModelRankingSummary(24)
	require.Error(t, err)
}

func TestModelRankingVisibilityUsesCatalogNameRules(t *testing.T) {
	metadata := []gatewayschema.Model{
		{ModelName: "secret-public", NameRule: gatewayschema.NameRuleExact, Status: 1},
		{ModelName: "secret-", NameRule: gatewayschema.NameRulePrefix, Status: 0},
		{ModelName: "-hidden", NameRule: gatewayschema.NameRuleSuffix, Status: 0},
		{ModelName: "internal", NameRule: gatewayschema.NameRuleContains, Status: 0},
	}
	for _, name := range []string{"secret-a", "ordinary-hidden", "some-internal-model"} {
		require.False(t, modelRankingVisible(name, metadata), name)
	}
	require.True(t, modelRankingVisible("secret-public", metadata))
	require.True(t, modelRankingVisible("ordinary", metadata))
}
