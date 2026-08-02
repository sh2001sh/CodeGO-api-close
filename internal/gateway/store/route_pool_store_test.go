package store

import (
	"testing"

	"github.com/glebarez/sqlite"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSaveRoutePool_ReplacesExistingMemberWithoutUniqueConflict(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gatewayschema.RoutePool{},
		&gatewayschema.RoutePoolMember{},
	))

	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() {
		platformdb.DB = originalDB
		InvalidateRoutePoolCache()
	})

	pool := gatewayschema.RoutePool{Name: "default 自动路由", Group: "default", Enabled: true}
	_, err = SaveRoutePool(&pool, []gatewayschema.RoutePoolMember{{
		ChannelID:      42,
		CostMultiplier: 0.1,
		Enabled:        true,
	}})
	require.NoError(t, err)

	_, err = SaveRoutePool(&pool, []gatewayschema.RoutePoolMember{{
		ChannelID:      42,
		CostMultiplier: 0.2,
		Enabled:        false,
	}})
	require.NoError(t, err)

	var members []gatewayschema.RoutePoolMember
	require.NoError(t, db.Where("route_pool_id = ?", pool.ID).Find(&members).Error)
	require.Len(t, members, 1)
	require.Equal(t, 42, members[0].ChannelID)
	require.Equal(t, 0.2, members[0].CostMultiplier)
	require.False(t, members[0].Enabled)
}

func TestUpdateRoutePoolMemberCostMultipliers_UpdatesEveryPoolMembership(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gatewayschema.RoutePool{},
		&gatewayschema.RoutePoolMember{},
	))

	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() {
		platformdb.DB = originalDB
		InvalidateRoutePoolCache()
	})

	first := gatewayschema.RoutePool{Name: "default", Group: "default", Enabled: true}
	second := gatewayschema.RoutePool{Name: "backup", Group: "backup", Enabled: true}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error)
	require.NoError(t, db.Create(&gatewayschema.RoutePoolMember{
		RoutePoolID: first.ID, ChannelID: 50, CostMultiplier: 0.06, ModelCostOverrides: `{"gpt-test":0.08}`, Enabled: true,
	}).Error)
	require.NoError(t, db.Create(&gatewayschema.RoutePoolMember{
		RoutePoolID: second.ID, ChannelID: 50, CostMultiplier: 0.06, ModelCostOverrides: `{}`, Enabled: false,
	}).Error)
	require.NoError(t, db.Model(&gatewayschema.RoutePoolMember{}).
		Where("route_pool_id = ? AND channel_id = ?", second.ID, 50).
		Update("enabled", false).Error)

	changed, missing, err := UpdateRoutePoolMemberCostMultipliers(map[int]float64{50: 0.01, 999: 0.02}, 0.000001)
	require.NoError(t, err)
	require.Equal(t, 2, changed)
	require.Equal(t, []int{999}, missing)

	var members []gatewayschema.RoutePoolMember
	require.NoError(t, db.Where("channel_id = ?", 50).Order("route_pool_id asc").Find(&members).Error)
	require.Len(t, members, 2)
	for _, member := range members {
		require.InDelta(t, 0.01, member.CostMultiplier, 0.000001)
	}
	require.Equal(t, `{"gpt-test":0.08}`, members[0].ModelCostOverrides)
	require.False(t, members[1].Enabled)
}
