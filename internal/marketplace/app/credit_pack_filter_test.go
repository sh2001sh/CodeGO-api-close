package app

import (
	"testing"

	gatewaygroups "github.com/sh2001sh/new-api/internal/gateway/groupsettings"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCreditPackFilterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openMarketplaceAppTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	oldLogDB, oldRedis := platformdb.LogDB, platformcache.RedisEnabled
	oldGroups := gatewaygroups.UserUsableGroups2JSONString()
	platformdb.LogDB, platformcache.RedisEnabled = nil, false
	t.Cleanup(func() {
		platformdb.LogDB, platformcache.RedisEnabled = oldLogDB, oldRedis
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(oldGroups))
		marketplaceListCache.entries = nil
	})
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"pack-official":"Official"}`))
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{}, &marketplaceschema.GroupAccess{}, &marketplaceschema.RankingSnapshot{},
		&marketplaceschema.ChannelFeedback{}, &marketplaceschema.ChannelUserBlock{},
		&marketplaceschema.AutoRoutePoolMember{}, &marketplaceschema.AutoRoutePoolConfig{},
		&marketplaceschema.RoutePool{}, &marketplaceschema.RoutePoolMember{},
		&gatewayschema.Channel{}, &gatewayschema.Ability{}, &identityschema.User{},
	))
	require.NoError(t, db.Create(&identityschema.User{Id: 42, Username: "pack-filter-owner", Password: "test", Group: "default"}).Error)
	require.NoError(t, db.Create(&gatewayschema.Channel{Id: 1801, Status: 1, Key: "test", ChannelScope: gatewayschema.ChannelScopeOfficial}).Error)
	require.NoError(t, db.Create(&gatewayschema.Ability{Group: "pack-official", Model: "gpt-5", ChannelId: 1801, Enabled: true}).Error)
	return db
}

func seedCreditPackFilterChannel(t *testing.T, db *gorm.DB, id string, owner, internalID int, supported, private bool) {
	t.Helper()
	channel := marketplaceschema.Channel{ID: id, OwnerUserID: owner, DeclaredModels: `["gpt-5"]`,
		InternalChannelID: &internalID, Status: marketplacedomain.LifecycleActive,
		CreditPackSupported: supported, MultiplierCardSupported: !supported,
	}
	group := autoRouteTestGroup(id, id, owner, 1)
	if private {
		group.Visibility = marketplacedomain.VisibilityPrivate
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)
}

func TestCreditPackMarketFilterKeepsOfficialAndOppositeMultiplierCardPolicy(t *testing.T) {
	db := setupCreditPackFilterTestDB(t)
	seedCreditPackFilterChannel(t, db, "pack-supported", 10, 1802, true, false)
	seedCreditPackFilterChannel(t, db, "pack-unsupported", 11, 1803, false, false)
	for _, tc := range []struct {
		query GroupQuery
		ids   []string
	}{
		{GroupQuery{CreditPack: "supported"}, []string{"pack-supported", "official:pack-official"}},
		{GroupQuery{CreditPack: "unsupported"}, []string{"pack-unsupported"}},
		{GroupQuery{CreditPack: "supported", MultiplierCard: "unsupported"}, []string{"pack-supported", "official:pack-official"}},
		{GroupQuery{CreditPack: "unsupported", MultiplierCard: "supported"}, []string{"pack-unsupported"}},
		{GroupQuery{CreditPack: " SUPPORTED "}, []string{"pack-supported", "official:pack-official"}},
	} {
		result, err := ListMarketplaceGroups(tc.query)
		require.NoError(t, err)
		ids := make([]string, 0, len(result.Items))
		for _, item := range result.Items {
			ids = append(ids, item.ID)
			require.Equal(t, item.ID != "pack-unsupported", item.CreditPackSupported)
		}
		require.ElementsMatch(t, tc.ids, ids)
	}
	for _, filter := range []string{"supported", "unsupported"} {
		shops, err := ListMarketplaceShops(GroupQuery{CreditPack: filter})
		require.NoError(t, err)
		require.Len(t, shops.Items, 1)
		if filter == "supported" {
			require.Equal(t, 10, shops.Items[0].ID)
		} else {
			require.Equal(t, 11, shops.Items[0].ID)
		}
	}
}

func TestCreditPackAutoBuildPersistsAndFiltersPrimaryAndExploreCandidates(t *testing.T) {
	db := setupCreditPackFilterTestDB(t)
	seedCreditPackFilterChannel(t, db, "pack-own-private", 42, 1812, true, true)
	seedCreditPackFilterChannel(t, db, "pack-public", 10, 1813, true, false)
	// With equal scores this unsupported candidate sorts first, so it would
	// occupy the primary slot if filtering happened only during exploration.
	seedCreditPackFilterChannel(t, db, "00-pack-excluded", 11, 1814, false, false)
	view, err := CreateRoutePool(42, RoutePoolCreateRequest{Name: "套餐池", AutoBuild: &RoutePoolAutoBuildConfig{
		Enabled: false, CreditPackOnly: true, Models: []string{"gpt-5"}, Size: 1, Explore: 10,
	}})
	require.NoError(t, err)
	require.True(t, view.AutoBuild.CreditPackOnly)
	view, err = ListRoutePool(42, view.ID)
	require.NoError(t, err)
	require.True(t, view.AutoBuild.CreditPackOnly)
	require.False(t, view.AutoBuild.Enabled)
	view, err = RunRoutePoolAutoBuild(42, view.ID)
	require.NoError(t, err)
	selected := make([]string, 0)
	for _, item := range view.Items {
		if item.Selected {
			require.True(t, item.CreditPackSupported)
			selected = append(selected, item.GroupID)
		}
	}
	require.ElementsMatch(t, []string{"pack-own-private", "pack-public", "official:pack-official"}, selected)
	require.NotNil(t, view.AutoBuild.LastBuiltAt)
	var pool marketplaceschema.RoutePool
	require.NoError(t, db.First(&pool, "id = ?", view.ID).Error)
	require.True(t, pool.AutoBuildCreditPackOnly)
	view, err = UpdateRoutePool(42, view.ID, RoutePoolUpdateRequest{GroupIDs: selected, AutoBuild: &RoutePoolAutoBuildConfig{
		CreditPackOnly: false, Models: []string{"gpt-5"}, Size: 10,
	}})
	require.NoError(t, err)
	require.False(t, view.AutoBuild.CreditPackOnly)
	view, err = RunRoutePoolAutoBuild(42, view.ID)
	require.NoError(t, err)
	require.Equal(t, 4, view.SelectedCount)
	var saved marketplaceschema.RoutePool
	require.NoError(t, db.First(&saved, "id = ?", view.ID).Error)
	require.False(t, saved.AutoBuildCreditPackOnly)
}
