package http

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformops "github.com/sh2001sh/new-api/internal/platform/opssettings"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"

	"github.com/sh2001sh/new-api/setting/config"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type listModelsResponse struct {
	Success bool               `json:"success"`
	Data    []dto.OpenAIModels `json:"data"`
	Object  string             `json:"object"`
}

func setupModelListControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	initModelListColumnNames(t)

	gin.SetMode(gin.TestMode)
	platformdb.UsingSQLite = true
	platformdb.UsingMySQL = false
	platformdb.UsingPostgreSQL = false
	platformcache.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	platformdb.DB = db
	platformdb.LogDB = db

	require.NoError(t, db.AutoMigrate(
		&identityschema.User{}, &identityschema.Token{}, &auditschema.Log{},
		&gatewayschema.Channel{}, &gatewayschema.Ability{}, &gatewayschema.Model{},
		&gatewayschema.Vendor{}, &billingschema.BillingAccount{},
		&commerceschema.UserSubscription{},
		&marketplaceschema.Channel{}, &marketplaceschema.Group{},
		&marketplaceschema.AutoRoutePoolMember{}, &marketplaceschema.RoutePool{},
		&marketplaceschema.RoutePoolMember{},
	))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func initModelListColumnNames(t *testing.T) {
	t.Helper()

	originalIsMasterNode := platformconfig.IsMasterNode
	originalSQLitePath := platformdb.SQLitePath
	originalUsingSQLite := platformdb.UsingSQLite
	originalUsingMySQL := platformdb.UsingMySQL
	originalUsingPostgreSQL := platformdb.UsingPostgreSQL
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")
	defer func() {
		platformconfig.IsMasterNode = originalIsMasterNode
		platformdb.SQLitePath = originalSQLitePath
		platformdb.UsingSQLite = originalUsingSQLite
		platformdb.UsingMySQL = originalUsingMySQL
		platformdb.UsingPostgreSQL = originalUsingPostgreSQL
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	}()

	platformconfig.IsMasterNode = false
	platformdb.SQLitePath = fmt.Sprintf("file:%s_init?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	platformdb.UsingSQLite = false
	platformdb.UsingMySQL = false
	platformdb.UsingPostgreSQL = false
	require.NoError(t, os.Setenv("SQL_DSN", "local"))

	require.NoError(t, platformstore.InitPrimaryDB())
	if platformdb.DB != nil {
		sqlDB, err := platformdb.DB.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
}

func withTieredBillingConfig(t *testing.T, modes map[string]string, exprs map[string]string) {
	t.Helper()

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "billing_setting.") {
			saved[key] = value
		}
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		gatewaystore.InvalidatePricingCache()
	})

	modeBytes, err := platformencoding.Marshal(modes)
	require.NoError(t, err)
	exprBytes, err := platformencoding.Marshal(exprs)
	require.NoError(t, err)

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": string(modeBytes),
		"billing_setting.billing_expr": string(exprBytes),
	}))
	gatewaystore.InvalidatePricingCache()
}

func withSelfUseModeDisabled(t *testing.T) {
	t.Helper()

	original := platformops.IsSelfUseModeEnabled()
	platformops.SetSelfUseModeEnabled(false)
	t.Cleanup(func() {
		platformops.SetSelfUseModeEnabled(original)
	})
}

func decodeListModelsResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]struct{} {
	t.Helper()

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload listModelsResponse
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, "list", payload.Object)

	ids := make(map[string]struct{}, len(payload.Data))
	for _, item := range payload.Data {
		ids[item.Id] = struct{}{}
	}
	return ids
}

func pricingByModelName(pricings []gatewaydomain.Pricing) map[string]gatewaydomain.Pricing {
	byName := make(map[string]gatewaydomain.Pricing, len(pricings))
	for _, pricing := range pricings {
		byName[pricing.ModelName] = pricing
	}
	return byName
}

func TestListModelsIncludesTieredBillingModel(t *testing.T) {
	withSelfUseModeDisabled(t)
	withTieredBillingConfig(t, map[string]string{
		"zz-tiered-visible-model":      "tiered_expr",
		"zz-tiered-empty-expr-model":   "tiered_expr",
		"zz-tiered-missing-expr-model": "tiered_expr",
	}, map[string]string{
		"zz-tiered-visible-model":    `tier("base", p * 1 + c * 2)`,
		"zz-tiered-empty-expr-model": "   ",
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&identityschema.User{
		Id:       1001,
		Username: "model-list-user",
		Password: "password",
		Group:    "default",
		Status:   constant.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]gatewayschema.Ability{
		{Group: "default", Model: "zz-tiered-visible-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-tiered-empty-expr-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-tiered-missing-expr-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-unpriced-model", ChannelId: 1, Enabled: true},
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx.Set("id", 1001)

	ListModels(ctx, constant.ChannelTypeOpenAI)

	ids := decodeListModelsResponse(t, recorder)
	require.Contains(t, ids, "zz-tiered-visible-model")
	require.NotContains(t, ids, "zz-tiered-empty-expr-model")
	require.NotContains(t, ids, "zz-tiered-missing-expr-model")
	require.NotContains(t, ids, "zz-unpriced-model")

	pricingByName := pricingByModelName(gatewaystore.LoadPricing())
	visiblePricing, ok := pricingByName["zz-tiered-visible-model"]
	require.True(t, ok)
	require.Equal(t, "tiered_expr", visiblePricing.BillingMode)
	require.NotEmpty(t, visiblePricing.BillingExpr)

	emptyExprPricing, ok := pricingByName["zz-tiered-empty-expr-model"]
	require.True(t, ok)
	require.Empty(t, emptyExprPricing.BillingMode)
	require.Empty(t, emptyExprPricing.BillingExpr)

	missingExprPricing, ok := pricingByName["zz-tiered-missing-expr-model"]
	require.True(t, ok)
	require.Empty(t, missingExprPricing.BillingMode)
	require.Empty(t, missingExprPricing.BillingExpr)
}

func TestListModelsTokenLimitIncludesTieredBillingModel(t *testing.T) {
	withSelfUseModeDisabled(t)
	withTieredBillingConfig(t, map[string]string{
		"zz-token-tiered-visible-model":      "tiered_expr",
		"zz-token-tiered-empty-expr-model":   "tiered_expr",
		"zz-token-tiered-missing-expr-model": "tiered_expr",
	}, map[string]string{
		"zz-token-tiered-visible-model":    `tier("base", p * 1 + c * 2)`,
		"zz-token-tiered-empty-expr-model": "",
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	httpctx.SetContextKey(ctx, constant.ContextKeyTokenModelLimitEnabled, true)
	httpctx.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, map[string]bool{
		"zz-token-tiered-visible-model":      true,
		"zz-token-tiered-empty-expr-model":   true,
		"zz-token-tiered-missing-expr-model": true,
		"zz-token-unpriced-model":            true,
	})

	ListModels(ctx, constant.ChannelTypeOpenAI)

	ids := decodeListModelsResponse(t, recorder)
	require.Contains(t, ids, "zz-token-tiered-visible-model")
	require.NotContains(t, ids, "zz-token-tiered-empty-expr-model")
	require.NotContains(t, ids, "zz-token-tiered-missing-expr-model")
	require.NotContains(t, ids, "zz-token-unpriced-model")
}

func TestListModelsNormalizesLegacyAbilityWhitespace(t *testing.T) {
	withSelfUseModeDisabled(t)
	withTieredBillingConfig(t, map[string]string{
		"zz-spaced-model": "tiered_expr",
	}, map[string]string{
		"zz-spaced-model": `tier("base", p * 1 + c * 2)`,
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&identityschema.User{
		Id:       1002,
		Username: "model-list-spaced-user",
		Password: "password",
		Group:    "default",
		Status:   constant.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&gatewayschema.Ability{
		Group: " default ", Model: " zz-spaced-model ", ChannelId: 1, Enabled: true,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx.Set("id", 1002)

	ListModels(ctx, constant.ChannelTypeOpenAI)

	ids := decodeListModelsResponse(t, recorder)
	require.Contains(t, ids, "zz-spaced-model")
}

func TestListModelsUsesResolvedMarketplaceGroup(t *testing.T) {
	withSelfUseModeDisabled(t)
	withTieredBillingConfig(t, map[string]string{
		"zz-market-model": "tiered_expr",
	}, map[string]string{
		"zz-market-model": `tier("base", p * 1 + c * 2)`,
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&identityschema.User{
		Id:       1003,
		Username: "model-list-market-user",
		Password: "password",
		Group:    "default",
		Status:   constant.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&gatewayschema.Ability{
		Group: "market_internal_42", Model: "zz-market-model", ChannelId: 1, Enabled: true,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx.Set("id", 1003)
	httpctx.SetContextKey(ctx, constant.ContextKeyTokenGroup, "market:42")
	httpctx.SetContextKey(ctx, constant.ContextKeyUsingGroup, "market_internal_42")

	ListModels(ctx, constant.ChannelTypeOpenAI)

	ids := decodeListModelsResponse(t, recorder)
	require.Contains(t, ids, "zz-market-model")
}

func TestListModelsKeepsMarketplaceGroupWithoutResolvedContext(t *testing.T) {
	withSelfUseModeDisabled(t)
	withTieredBillingConfig(t, map[string]string{
		"zz-market-direct-model": "tiered_expr",
	}, map[string]string{
		"zz-market-direct-model": `tier("base", p * 1 + c * 2)`,
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&identityschema.User{
		Id:       1004,
		Username: "model-list-market-direct-user",
		Password: "password",
		Group:    "default",
		Status:   constant.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&gatewayschema.Ability{
		Group: "market:direct", Model: "zz-market-direct-model", ChannelId: 1, Enabled: true,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx.Set("id", 1004)
	httpctx.SetContextKey(ctx, constant.ContextKeyTokenGroup, "market:direct")

	ListModels(ctx, constant.ChannelTypeOpenAI)

	ids := decodeListModelsResponse(t, recorder)
	require.Contains(t, ids, "zz-market-direct-model")
}

func TestListModelsUsesNamedRoutePoolInsteadOfGlobalAutoPool(t *testing.T) {
	withSelfUseModeDisabled(t)
	db := setupModelListControllerTestDB(t)
	const userID = 1005
	require.NoError(t, db.Create(&identityschema.User{
		Id: userID, Username: "named-route-pool-user", Password: "password",
		Group: "default", Status: constant.UserStatusEnabled,
	}).Error)

	globalChannelID, namedChannelID := 11, 12
	require.NoError(t, db.Create([]marketplaceschema.Channel{
		{ID: "global-codex-channel", OwnerUserID: 501, ProviderType: "openai", DeclaredModels: `["zz-global-codex-model"]`, InternalChannelID: &globalChannelID, Status: "active"},
		{ID: "named-cc-channel", OwnerUserID: 502, ProviderType: "anthropic", DeclaredModels: `["zz-named-cc-model"]`, InternalChannelID: &namedChannelID, Status: "active"},
	}).Error)
	require.NoError(t, db.Create([]marketplaceschema.Group{
		{ID: "global-codex-group", ChannelID: "global-codex-channel", OwnerUserID: 501, PublicSlug: "global-codex", SystemDisplayName: "global-codex", InternalGroupName: "global-codex-internal", SourceType: "marketplace_user", CreditPoolPolicy: "marketplace_universal_only", Multiplier: 1, LifecycleStatus: "active", VerificationStatus: "passed", Visibility: "public"},
		{ID: "named-cc-group", ChannelID: "named-cc-channel", OwnerUserID: 502, PublicSlug: "named-cc", SystemDisplayName: "named-cc", InternalGroupName: "named-cc-internal", SourceType: "marketplace_user", CreditPoolPolicy: "marketplace_universal_only", Multiplier: 1, LifecycleStatus: "active", VerificationStatus: "passed", Visibility: "public"},
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.AutoRoutePoolMember{OwnerUserID: userID, GroupID: "global-codex-group", Priority: 1}).Error)
	require.NoError(t, db.Create(&marketplaceschema.RoutePool{ID: "named-cc-pool", OwnerUserID: userID, Name: "CC pool", Strategy: "priority", MaxAttempts: 3, FailureCooldownSeconds: 30}).Error)
	require.NoError(t, db.Create(&marketplaceschema.RoutePoolMember{PoolID: "named-cc-pool", GroupID: "named-cc-group", Priority: 1}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx.Set("id", userID)
	// Authentication intentionally leaves route-pool requests on market:auto
	// until a model is selected; the original bug replaced the named pool here.
	httpctx.SetContextKey(ctx, constant.ContextKeyTokenGroup, "market:pool:named-cc-pool")
	httpctx.SetContextKey(ctx, constant.ContextKeyUsingGroup, "market:auto")

	ListModels(ctx, constant.ChannelTypeOpenAI)

	ids := decodeListModelsResponse(t, recorder)
	require.Contains(t, ids, "zz-named-cc-model")
	require.NotContains(t, ids, "zz-global-codex-model")
}
