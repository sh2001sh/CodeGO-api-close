package app

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupRoutePoolAdminTestDB(t *testing.T) *gorm.DB {
	t.Helper()
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
	require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}, &gatewayschema.RoutePool{}, &gatewayschema.RoutePoolMember{}))
	return db
}

func TestListRoutePoolGroupsExcludesMarketplaceChannels(t *testing.T) {
	db := setupRoutePoolAdminTestDB(t)
	require.NoError(t, db.Create(&gatewayschema.Channel{
		Id: 1, Type: 1, Key: "official", Name: "official-channel",
		Status: constant.ChannelStatusEnabled, Models: "gpt-5", Group: "official-group",
		ChannelScope: gatewayschema.ChannelScopeOfficial,
	}).Error)
	require.NoError(t, db.Create(&gatewayschema.Channel{
		Id: 2, Type: 1, Key: "external", Name: "marketplace-channel",
		Status: constant.ChannelStatusEnabled, Models: "gpt-5", Group: "marketplace-group",
		ChannelScope: gatewayschema.ChannelScopeExternal,
	}).Error)

	groups, err := ListRoutePoolGroups()
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, "official-group", groups[0].Group)
	require.Len(t, groups[0].Channels, 1)
	require.Equal(t, 1, groups[0].Channels[0].ChannelID)
}

func TestSaveRoutePoolGroupRejectsMarketplaceChannelMember(t *testing.T) {
	db := setupRoutePoolAdminTestDB(t)
	require.NoError(t, db.Create(&gatewayschema.Channel{
		Id: 11, Type: 1, Key: "official", Name: "official-channel",
		Status: constant.ChannelStatusEnabled, Models: "gpt-5", Group: "shared-group",
		ChannelScope: gatewayschema.ChannelScopeOfficial,
	}).Error)
	require.NoError(t, db.Create(&gatewayschema.Channel{
		Id: 12, Type: 1, Key: "external", Name: "marketplace-channel",
		Status: constant.ChannelStatusEnabled, Models: "gpt-5", Group: "shared-group",
		ChannelScope: gatewayschema.ChannelScopeExternal,
	}).Error)

	_, err := SaveRoutePoolGroup("shared-group", true, []gatewayschema.RoutePoolMember{
		{ChannelID: 12, Enabled: true, CostMultiplier: 1, ModelCostOverrides: "{}"},
	})
	require.EqualError(t, err, "route member is not assigned to this group")
}

func TestRoutePoolMetricsPreserveTTFTPenalty(t *testing.T) {
	db := setupRoutePoolAdminTestDB(t)
	require.NoError(t, db.AutoMigrate(&gatewayschema.Ability{}))
	oldMemory := platformconfig.MemoryCacheEnabled
	platformconfig.MemoryCacheEnabled = false
	t.Cleanup(func() { platformconfig.MemoryCacheEnabled = oldMemory })
	const model = "gpt-admin-ttft"
	channels := []gatewayschema.Channel{
		{Id: 8_400_001, Key: "test", Status: constant.ChannelStatusEnabled, Group: "metrics-ttft", Models: model},
		{Id: 8_400_002, Key: "test", Status: constant.ChannelStatusEnabled, Group: "metrics-ttft", Models: model},
	}
	for index := range channels {
		require.NoError(t, db.Create(&channels[index]).Error)
		require.NoError(t, gatewaystore.AddChannelAbilities(&channels[index], db))
	}
	pool, err := gatewaystore.SaveRoutePool(&gatewayschema.RoutePool{Name: "metrics-ttft", Group: "metrics-ttft", Enabled: true}, []gatewayschema.RoutePoolMember{
		{ChannelID: channels[0].Id, Enabled: true, CostMultiplier: 1},
		{ChannelID: channels[1].Id, Enabled: true, CostMultiplier: 1},
	})
	require.NoError(t, err)
	gatewayruntime.RecordChannelSuccess(channels[0].Id, model, 100*time.Millisecond, gatewayruntime.RequestTypeChatShortStream)
	gatewayruntime.RecordChannelSuccess(channels[1].Id, model, time.Second, gatewayruntime.RequestTypeChatShortStream)
	metrics, err := GetRoutePoolMetrics(pool.Pool.ID, model)
	require.NoError(t, err)
	require.Len(t, metrics.Members, 2)
	for _, member := range metrics.Members {
		require.True(t, member.Eligible)
		baseScore := effectiveRoutePoolCostForPool(pool.Members[0], model, member.Health, &pool.Pool)
		if member.ChannelID == channels[1].Id {
			require.InDelta(t, baseScore*2, member.Score, 0.00001, "slow member retains its TTFT penalty")
		} else {
			require.InDelta(t, baseScore, member.Score, 0.00001)
		}
	}
}
