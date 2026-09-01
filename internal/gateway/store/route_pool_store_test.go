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

func TestResolveEnabledRoutePoolAliasReturnsConcreteGroupOnlyWhenEnabled(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gatewayschema.RoutePool{}))

	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() {
		platformdb.DB = originalDB
		InvalidateRoutePoolCache()
	})

	require.NoError(t, db.Create(&gatewayschema.RoutePool{
		Name: "路由池1", Group: "default", Enabled: true,
	}).Error)
	var disabledPool gatewayschema.RoutePool
	disabledPool = gatewayschema.RoutePool{Name: "已停用池", Group: "default"}
	require.NoError(t, db.Create(&disabledPool).Error)
	require.NoError(t, db.Model(&gatewayschema.RoutePool{}).Where("id = ?", disabledPool.ID).Update("enabled", false).Error)

	group, found, err := ResolveEnabledRoutePoolAlias("路由池1")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "default", group)

	group, found, err = ResolveEnabledRoutePoolAlias("已停用池")
	require.NoError(t, err)
	require.False(t, found)
	require.Empty(t, group)
}

func TestListSelectableRoutePoolsReturnsOnlyEnabledRedactedRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gatewayschema.RoutePool{}, &gatewayschema.RoutePoolMember{}))
	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })

	enabled := gatewayschema.RoutePool{Name: "路由池1", Group: "default", Enabled: true, ModelScope: "gpt-test"}
	disabled := gatewayschema.RoutePool{Name: "停用池", Group: "backup", Enabled: false}
	require.NoError(t, db.Create(&enabled).Error)
	require.NoError(t, db.Create(&disabled).Error)
	require.NoError(t, db.Model(&gatewayschema.RoutePool{}).Where("id = ?", disabled.ID).Update("enabled", false).Error)
	require.NoError(t, db.Create(&gatewayschema.RoutePoolMember{
		RoutePoolID: enabled.ID, ChannelID: 7, CostMultiplier: 1, Enabled: true,
	}).Error)
	require.NoError(t, db.Create(&gatewayschema.RoutePoolMember{
		RoutePoolID: enabled.ID, ChannelID: 8, CostMultiplier: 1, Enabled: false,
	}).Error)
	require.NoError(t, db.Model(&gatewayschema.RoutePoolMember{}).
		Where("route_pool_id = ? AND channel_id = ?", enabled.ID, 8).
		Update("enabled", false).Error)

	items, err := ListSelectableRoutePools()
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, SelectableRoutePool{Name: "路由池1", Group: "default", ModelScope: "gpt-test", MemberCount: 1}, items[0])
}

func TestListRoutePoolsTreatsMissingLegacyTableAsEmpty(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })

	items, err := ListRoutePools()
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestSaveRoutePoolRejectsDuplicateNames(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gatewayschema.RoutePool{}, &gatewayschema.RoutePoolMember{}))

	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() {
		platformdb.DB = originalDB
		InvalidateRoutePoolCache()
	})

	first := gatewayschema.RoutePool{Name: "route-pool", Group: "default", Enabled: true}
	_, err = SaveRoutePool(&first, nil)
	require.NoError(t, err)

	_, err = SaveRoutePool(&gatewayschema.RoutePool{Name: "route-pool", Group: "backup", Enabled: true}, nil)
	require.EqualError(t, err, "route pool name already exists")
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

func TestUpdateRoutePoolMemberFaultDomains_UpdatesEveryPoolMembership(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gatewayschema.RoutePool{}, &gatewayschema.RoutePoolMember{}))
	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() {
		platformdb.DB = originalDB
		InvalidateRoutePoolCache()
	})

	require.NoError(t, db.Create(&gatewayschema.RoutePoolMember{RoutePoolID: 1, ChannelID: 50, CostMultiplier: 1, FaultDomain: "provider:primary"}).Error)
	require.NoError(t, db.Create(&gatewayschema.RoutePoolMember{RoutePoolID: 2, ChannelID: 50, CostMultiplier: 1, FaultDomain: "provider:primary"}).Error)
	changed, missing, err := UpdateRoutePoolMemberFaultDomains(map[int]string{50: "provider:secondary", 999: "provider:unknown"})
	require.NoError(t, err)
	require.Equal(t, 2, changed)
	require.Equal(t, []int{999}, missing)

	var members []gatewayschema.RoutePoolMember
	require.NoError(t, db.Where("channel_id = ?", 50).Find(&members).Error)
	require.Len(t, members, 2)
	for _, member := range members {
		require.Equal(t, "provider:secondary", member.FaultDomain)
	}
}
