package store

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelHasExclusiveEnabledAbility(t *testing.T) {
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
		platformdb.UsingPostgreSQL = originalPostgreSQL
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gatewayschema.Ability{}))
	platformdb.DB = db
	platformdb.UsingSQLite = true
	platformdb.UsingPostgreSQL = false

	require.NoError(t, db.Create(&gatewayschema.Ability{Group: "free", Model: "shared", ChannelId: 1, Enabled: true}).Error)
	require.NoError(t, db.Create(&gatewayschema.Ability{Group: "free", Model: "shared", ChannelId: 2, Enabled: true}).Error)
	require.NoError(t, db.Create(&gatewayschema.Ability{Group: "free", Model: "exclusive", ChannelId: 1, Enabled: true}).Error)
	require.NoError(t, db.Create(&gatewayschema.Ability{Group: "vip", Model: "shared", ChannelId: 1, Enabled: true}).Error)
	require.NoError(t, db.Create(&gatewayschema.Ability{Group: "vip", Model: "shared", ChannelId: 3, Enabled: false}).Error)

	exclusive, err := ChannelHasExclusiveEnabledAbility(1)
	require.NoError(t, err)
	require.True(t, exclusive)

	exclusive, err = ChannelHasExclusiveEnabledAbility(2)
	require.NoError(t, err)
	require.False(t, exclusive)

	alternative, err := HasAlternativeEnabledAbility(1, "free", "shared")
	require.NoError(t, err)
	require.True(t, alternative)

	alternative, err = HasAlternativeEnabledAbility(1, "free", "exclusive")
	require.NoError(t, err)
	require.False(t, alternative)
}

func TestHasAlternativeSelectableRouteHonorsRoutePoolMembership(t *testing.T) {
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
		platformdb.UsingPostgreSQL = originalPostgreSQL
		InvalidateRoutePoolCache()
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gatewayschema.Ability{},
		&gatewayschema.Channel{},
		&gatewayschema.RoutePool{},
		&gatewayschema.RoutePoolMember{},
	))
	platformdb.DB = db
	platformdb.UsingSQLite = true
	platformdb.UsingPostgreSQL = false
	InvalidateRoutePoolCache()

	priority := int64(1)
	for _, channelID := range []int{1, 2} {
		require.NoError(t, db.Create(&gatewayschema.Channel{
			Id:       channelID,
			Name:     "test",
			Type:     1,
			Status:   constant.ChannelStatusEnabled,
			Group:    "default",
			Models:   "gpt-test",
			Priority: &priority,
		}).Error)
		require.NoError(t, db.Create(&gatewayschema.Ability{
			Group: "default", Model: "gpt-test", ChannelId: channelID, Enabled: true,
		}).Error)
	}

	legacyAlternative, err := HasAlternativeSelectableRoute(1, "default", "gpt-test")
	require.NoError(t, err)
	require.True(t, legacyAlternative)

	pool := gatewayschema.RoutePool{Name: "default", Group: "default", Enabled: true}
	require.NoError(t, db.Create(&pool).Error)
	require.NoError(t, db.Create(&gatewayschema.RoutePoolMember{
		RoutePoolID: pool.ID, ChannelID: 1, CostMultiplier: 0.05, Enabled: true,
	}).Error)

	alternative, err := HasAlternativeSelectableRoute(1, "default", "gpt-test")
	require.NoError(t, err)
	require.False(t, alternative)

	require.NoError(t, db.Create(&gatewayschema.RoutePoolMember{
		RoutePoolID: pool.ID, ChannelID: 2, CostMultiplier: 0.05, Enabled: true,
	}).Error)
	require.NoError(t, db.Model(&gatewayschema.RoutePoolMember{}).
		Where("route_pool_id = ? AND channel_id = ?", pool.ID, 2).
		Update("enabled", false).Error)
	InvalidateRoutePoolCache()
	alternative, err = HasAlternativeSelectableRoute(1, "default", "gpt-test")
	require.NoError(t, err)
	require.False(t, alternative)

	require.NoError(t, db.Model(&gatewayschema.RoutePoolMember{}).
		Where("route_pool_id = ? AND channel_id = ?", pool.ID, 2).
		Update("enabled", true).Error)
	InvalidateRoutePoolCache()
	alternative, err = HasAlternativeSelectableRoute(1, "default", "gpt-test")
	require.NoError(t, err)
	require.True(t, alternative)
}
