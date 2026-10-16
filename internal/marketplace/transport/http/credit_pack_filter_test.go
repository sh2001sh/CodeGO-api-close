package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	gatewaygroups "github.com/sh2001sh/new-api/internal/gateway/groupsettings"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupCreditPackFilterAPITest(t *testing.T) *gin.Engine {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	oldDB, oldLogDB := platformdb.DB, platformdb.LogDB
	oldPG, oldSQLite, oldRedis := platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformcache.RedisEnabled
	oldGroups := gatewaygroups.UserUsableGroups2JSONString()
	platformdb.DB, platformdb.LogDB = db, nil
	platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformcache.RedisEnabled = false, true, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.LogDB = oldDB, oldLogDB
		platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformcache.RedisEnabled = oldPG, oldSQLite, oldRedis
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(oldGroups))
	})
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Group{}, &marketplaceschema.Channel{}, &marketplaceschema.GroupAccess{},
		&marketplaceschema.RankingSnapshot{}, &marketplaceschema.ChannelFeedback{}, &marketplaceschema.ChannelUserBlock{},
		&marketplaceschema.Shop{}, &marketplaceschema.ShopRating{}, &marketplaceschema.MarketReport{},
		&marketplaceschema.UserMultiplier{}, &marketplaceschema.AutoRoutePoolMember{}, &marketplaceschema.AutoRoutePoolConfig{},
		&marketplaceschema.RoutePool{}, &marketplaceschema.RoutePoolMember{},
		&gatewayschema.Channel{}, &gatewayschema.Ability{}, &identityschema.User{},
	))
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"api-official":"Official"}`))
	require.NoError(t, db.Create(&identityschema.User{Id: 42, Username: "api-pool-owner", Password: "test", Group: "default"}).Error)
	require.NoError(t, db.Create(&gatewayschema.Channel{Id: 1901, Status: 1, Key: "test", ChannelScope: "official"}).Error)
	require.NoError(t, db.Create(&gatewayschema.Ability{Group: "api-official", Model: "gpt-5", ChannelId: 1901, Enabled: true}).Error)
	for index, id := range []string{"api-supported", "api-unsupported"} {
		internalID := 1902 + index
		require.NoError(t, db.Create(&marketplaceschema.Group{ID: id, ChannelID: id, OwnerUserID: 10 + index,
			InternalGroupName: id, PublicSlug: id, SourceType: "marketplace_user", Visibility: "public",
			LifecycleStatus: "active", VerificationStatus: "passed", Multiplier: 1,
		}).Error)
		require.NoError(t, db.Create(&marketplaceschema.Channel{ID: id, OwnerUserID: 10 + index, InternalChannelID: &internalID,
			DeclaredModels: `["gpt-5"]`, Status: "active", CreditPackSupported: index == 0, MultiplierCardSupported: index != 0,
		}).Error)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/groups", ListGroups)
	engine.GET("/shops", ListShops)
	pool := engine.Group("/pools", func(c *gin.Context) { c.Set("id", 42) })
	pool.POST("", CreateRoutePool)
	pool.GET("/:id", GetRoutePool)
	pool.PUT("/:id", UpdateRoutePool)
	pool.POST("/:id/run", RunRoutePoolAutoBuild)
	return engine
}

func creditPackFilterAPIRequest(t *testing.T, engine *gin.Engine, method, path, body string, data any) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success, response.Body.String())
	require.NoError(t, json.Unmarshal(result.Data, data))
}

func TestCreditPackFilterAPIParsesGroupsAndShopsIndependentlyFromCards(t *testing.T) {
	engine := setupCreditPackFilterAPITest(t)
	for _, tc := range []struct {
		query string
		ids   []string
	}{
		{"credit_pack=supported", []string{"api-supported", "official:api-official"}},
		{"credit_pack=unsupported", []string{"api-unsupported"}},
		{"credit_pack=supported&multiplier_card=unsupported", []string{"api-supported", "official:api-official"}},
		{"credit_pack=unsupported&multiplier_card=supported", []string{"api-unsupported"}},
	} {
		var groups marketplaceapp.GroupListResult
		creditPackFilterAPIRequest(t, engine, http.MethodGet, "/groups?"+tc.query, "", &groups)
		ids := make([]string, 0, len(groups.Items))
		for _, item := range groups.Items {
			ids = append(ids, item.ID)
		}
		require.ElementsMatch(t, tc.ids, ids)
	}
	for _, filter := range []string{"supported", "unsupported"} {
		var shops marketplaceapp.ShopList
		creditPackFilterAPIRequest(t, engine, http.MethodGet, "/shops?credit_pack="+filter, "", &shops)
		require.Len(t, shops.Items, 1)
		if filter == "supported" {
			require.Equal(t, 10, shops.Items[0].ID)
		} else {
			require.Equal(t, 11, shops.Items[0].ID)
		}
	}
}

func TestCreditPackAutoBuildAPISavesDisabledPlanAndRunsSavedFilter(t *testing.T) {
	engine := setupCreditPackFilterAPITest(t)
	var view marketplaceapp.RoutePoolView
	creditPackFilterAPIRequest(t, engine, http.MethodPost, "/pools",
		`{"name":"API套餐池","auto_build":{"enabled":false,"credit_pack_only":true,"models":["gpt-5"],"consumer_weight":40,"success_weight":30,"ttft_weight":20,"cache_weight":10,"size":1,"explore":10}}`, &view)
	require.True(t, view.AutoBuild.CreditPackOnly)
	path := "/pools/" + view.ID
	creditPackFilterAPIRequest(t, engine, http.MethodGet, path, "", &view)
	require.True(t, view.AutoBuild.CreditPackOnly)
	require.False(t, view.AutoBuild.Enabled)
	require.Equal(t, 40, view.AutoBuild.ConsumerWeight)
	require.Equal(t, []string{"gpt-5"}, view.AutoBuild.Models)
	creditPackFilterAPIRequest(t, engine, http.MethodPost, path+"/run", "", &view)
	require.Equal(t, 2, view.SelectedCount)
	for _, item := range view.Items {
		if item.Selected {
			require.True(t, item.CreditPackSupported)
			require.NotEqual(t, "api-unsupported", item.GroupID)
		}
	}
	creditPackFilterAPIRequest(t, engine, http.MethodPut, path,
		`{"auto_build":{"enabled":false,"credit_pack_only":false,"models":["gpt-5"],"size":10}}`, &view)
	creditPackFilterAPIRequest(t, engine, http.MethodGet, path, "", &view)
	require.False(t, view.AutoBuild.CreditPackOnly)
	creditPackFilterAPIRequest(t, engine, http.MethodPost, path+"/run", "", &view)
	require.Equal(t, 3, view.SelectedCount)
}
