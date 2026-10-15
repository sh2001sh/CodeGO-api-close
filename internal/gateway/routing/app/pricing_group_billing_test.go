package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBuildSub2APIKeyBillingResolvesGroupAndUserMultiplier(t *testing.T) {
	db := setupSub2APIBillingTestDB(t)
	require.NoError(t, db.Create(&identityschema.User{
		Id: 41, Username: "billing-user", Password: "password123", DisplayName: "Billing User",
		Group: "vip", AffCode: "SUB2API41",
	}).Error)

	originalRatios := gatewaystore.GroupRatio2JSONString()
	originalUserRatios := gatewaystore.GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, gatewaystore.UpdateGroupRatioByJSONString(originalRatios))
		require.NoError(t, gatewaystore.UpdateGroupGroupRatioByJSONString(originalUserRatios))
	})
	require.NoError(t, gatewaystore.UpdateGroupRatioByJSONString(`{"priced":1.25,"default":1}`))
	require.NoError(t, gatewaystore.UpdateGroupGroupRatioByJSONString(`{"vip":{"priced":0.8}}`))

	payload, err := BuildSub2APIKeyBilling(41, "priced")
	require.NoError(t, err)
	require.Equal(t, "sub2api.key_billing", payload.Object)
	require.Equal(t, 1, payload.SchemaVersion)
	require.Equal(t, "token", payload.BillingScope)
	require.Equal(t, "priced", payload.Group)
	require.InDelta(t, 1.25, payload.GroupRateMultiplier, 0.000001)
	require.InDelta(t, 0.8, payload.ResolvedRateMultiplier, 0.000001)
	require.InDelta(t, 0.8, payload.EffectiveRateMultiplier, 0.000001)
}

func TestBuildSub2APIKeyBillingSupportsAutoAndRoutePoolAlias(t *testing.T) {
	db := setupSub2APIBillingTestDB(t)
	require.NoError(t, db.Create(&identityschema.User{
		Id: 42, Username: "route-user", Password: "password123", DisplayName: "Route User",
		Group: "default", AffCode: "SUB2API42",
	}).Error)
	require.NoError(t, db.Create(&gatewayschema.RoutePool{Name: "快速池", Group: "routed", Enabled: true}).Error)
	gatewaystore.InvalidateRoutePoolCache()

	originalRatios := gatewaystore.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, gatewaystore.UpdateGroupRatioByJSONString(originalRatios))
		gatewaystore.InvalidateRoutePoolCache()
	})
	require.NoError(t, gatewaystore.UpdateGroupRatioByJSONString(`{"routed":0.6,"default":1}`))

	aliasPayload, err := BuildSub2APIKeyBilling(42, "快速池")
	require.NoError(t, err)
	require.Equal(t, "routed", aliasPayload.Group)
	require.InDelta(t, 0.6, aliasPayload.EffectiveRateMultiplier, 0.000001)

	autoPayload, err := BuildSub2APIKeyBilling(42, "")
	require.NoError(t, err)
	require.Equal(t, AutoGroupName, autoPayload.Group)
	require.InDelta(t, 1, autoPayload.GroupRateMultiplier, 0.000001)
	require.InDelta(t, 1, autoPayload.ResolvedRateMultiplier, 0.000001)
	require.InDelta(t, 1, autoPayload.EffectiveRateMultiplier, 0.000001)
}

func setupSub2APIBillingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	platformcache.RedisEnabled = false
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	originalDB := platformdb.DB
	platformdb.DB = db
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &gatewayschema.RoutePool{}))
	t.Cleanup(func() {
		platformdb.DB = originalDB
		if sqlDB, sqlErr := db.DB(); sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
