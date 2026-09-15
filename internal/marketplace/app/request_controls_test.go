package app

import (
	"errors"
	"fmt"
	"testing"

	gatewaygroups "github.com/sh2001sh/new-api/internal/gateway/groupsettings"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSavedPoolsRecheckChannelUserBlocks(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{},
		&marketplaceschema.RankingSnapshot{}, &marketplaceschema.ChannelUserBlock{}, &marketplaceschema.UserMultiplier{},
		&marketplaceschema.RoutePool{}, &marketplaceschema.RoutePoolMember{}, &marketplaceschema.AutoRoutePoolMember{}))
	for index, id := range []string{"blocked", "allowed"} {
		internalID := 701 + index
		require.NoError(t, db.Create(&marketplaceschema.Channel{ID: id, OwnerUserID: 11,
			DeclaredModels: `["gpt-5"]`, InternalChannelID: &internalID, Status: "active"}).Error)
		group := autoRouteTestGroup(id, id, 11, 1)
		require.NoError(t, db.Create(&group).Error)
		require.NoError(t, db.Create(&marketplaceschema.RoutePoolMember{PoolID: "saved", GroupID: id, Priority: index + 1}).Error)
		require.NoError(t, db.Create(&marketplaceschema.AutoRoutePoolMember{OwnerUserID: 20, GroupID: id, Priority: index + 1}).Error)
	}
	require.NoError(t, db.Create(&marketplaceschema.RoutePool{ID: "saved", OwnerUserID: 20, Name: "Saved", Strategy: "priority"}).Error)
	assertCandidates := func(expected []string) {
		t.Helper()
		auto, autoErr := ResolveAutoRouteBindings(20, "gpt-5", 0)
		named, _, namedErr := ResolveRoutePoolBindings(20, "saved", "gpt-5", 0)
		if len(expected) == 0 {
			require.Error(t, autoErr)
			require.Error(t, namedErr)
		} else {
			require.NoError(t, autoErr)
			require.NoError(t, namedErr)
		}
		for _, bindings := range [][]RoutingBinding{auto, named} {
			ids := make([]string, 0, len(bindings))
			for _, binding := range bindings {
				ids = append(ids, binding.GroupID)
			}
			require.Equal(t, expected, ids)
		}
	}
	assertCandidates([]string{"blocked", "allowed"})
	require.NoError(t, SetChannelUserBlock(11, "blocked", 20, true))
	_, err := ResolveTokenGroupBinding(TokenGroupValue("blocked"), 20)
	require.ErrorContains(t, err, "拉黑")
	assertCandidates([]string{"allowed"})
	groups, _, err := loadAutoRouteGroupsForIDs(21, []string{"blocked"})
	require.NoError(t, err)
	require.Len(t, groups, 1, "a block must not affect another user")
	require.NoError(t, SetChannelUserBlock(11, "allowed", 20, true))
	assertCandidates([]string{})
	models, err := ListRoutePoolModels(20, "saved")
	require.NoError(t, err)
	require.Empty(t, models)
	models, configured, err := ListSelectedAutoRouteModels(20)
	require.NoError(t, err)
	require.True(t, configured)
	require.Empty(t, models)
	require.NoError(t, SetChannelUserBlock(11, "blocked", 20, false))
	assertCandidates([]string{"blocked"})

	// A failed block lookup must not turn into permission to route.
	lookupErr := errors.New("block lookup unavailable")
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("fail_block_lookup", func(tx *gorm.DB) {
		if tx.Statement.Table == (marketplaceschema.ChannelUserBlock{}).TableName() {
			tx.AddError(lookupErr)
		}
	}))
	bindings, _, err := ResolveRoutePoolBindings(20, "saved", "gpt-5", 0)
	require.ErrorIs(t, err, lookupErr)
	require.Empty(t, bindings)
}

func TestSetChannelUserBlockByExternalID(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &marketplaceschema.Channel{}, &marketplaceschema.ChannelUserBlock{}))
	require.NoError(t, db.Create(&identityschema.User{Id: 20, ExternalId: "JLW7UE", Username: "blocked-user", Password: "password"}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Channel{ID: "420", OwnerUserID: 11}).Error)

	require.NoError(t, SetChannelUserBlockByExternalID(11, "420", " jlw7ue ", true))
	var block marketplaceschema.ChannelUserBlock
	require.NoError(t, db.Where("channel_id = ? AND user_id = ?", "420", 20).First(&block).Error)
	require.ErrorContains(t, SetChannelUserBlockByExternalID(11, "420", "missing", true), "用户编号不存在")
}

func TestSyncInternalChannelClearsZeroConcurrencyLimits(t *testing.T) {
	for _, limits := range []struct{ total, user int }{{0, 0}, {0, 2}, {7, 0}} {
		t.Run(fmt.Sprintf("total=%d/user=%d", limits.total, limits.user), func(t *testing.T) {
			db := openMarketplaceAppTestDB(t)
			require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}, &gatewayschema.Ability{}))
			internal := gatewayschema.Channel{Key: "encrypted", Name: "gate-test", MarketplaceMaxConcurrency: 10, MarketplaceUserMaxConcurrency: 3}
			require.NoError(t, db.Create(&internal).Error)
			channel := &marketplaceschema.Channel{ID: "gate-test", InternalChannelID: &internal.Id, ProviderType: "openai_compatible",
				DeclaredModels: `["gpt-5"]`, BaseURLCiphertext: "encrypted-url", CredentialCiphertext: "encrypted-key",
				MaxConcurrency: limits.total, UserMaxConcurrency: limits.user}
			group := &marketplaceschema.Group{ID: "gate-group", SystemDisplayName: "gate", InternalGroupName: "gate"}
			require.NoError(t, syncInternalChannel(channel, group))
			require.NoError(t, db.First(&internal, internal.Id).Error)
			require.Equal(t, limits.total, internal.MarketplaceMaxConcurrency)
			require.Equal(t, limits.user, internal.MarketplaceUserMaxConcurrency)
		})
	}
}

func TestOfficialConcurrencyAggregatesDistinctEnabledChannels(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	oldGroups := gatewaygroups.UserUsableGroups2JSONString()
	oldLogDB, oldRedis := platformdb.LogDB, platformcache.RedisEnabled
	platformdb.LogDB, platformcache.RedisEnabled = nil, false
	t.Cleanup(func() {
		platformdb.LogDB, platformcache.RedisEnabled = oldLogDB, oldRedis
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(oldGroups))
	})
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"official-test":"Official"}`))
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &gatewayschema.Channel{}, &gatewayschema.Ability{}))
	for _, channel := range []gatewayschema.Channel{{Id: 9801, Status: 1}, {Id: 9802, Status: 1}, {Id: 9803, Status: 2}, {Id: 9804, Status: 1}} {
		require.NoError(t, db.Create(&channel).Error)
		release := gatewayruntime.BeginChannelRequest(channel.Id)
		defer release()
		for _, model := range []string{"gpt-5", "gpt-5.1"} {
			require.NoError(t, db.Create(&gatewayschema.Ability{Group: "official-test", Model: model, ChannelId: channel.Id, Enabled: channel.Id != 9804}).Error)
		}
	}
	release := gatewayruntime.BeginChannelRequest(9801)
	items, err := listOfficialGroupStatus(0)
	release()
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, 3, items[0].CurrentConcurrency)
	items, err = listOfficialGroupStatus(0)
	require.NoError(t, err)
	require.Equal(t, 2, items[0].CurrentConcurrency, "released requests must disappear")
}
