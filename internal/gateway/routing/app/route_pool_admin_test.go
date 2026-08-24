package app

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
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
