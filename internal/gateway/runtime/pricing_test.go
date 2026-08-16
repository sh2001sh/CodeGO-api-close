package runtime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestZeroGroupRatioAlwaysSkipsBillingPreconsume(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRatios := gatewaystore.GroupRatio2JSONString()
	require.NoError(t, gatewaystore.UpdateGroupRatioByJSONString(`{"default":1,"free":0}`))
	t.Cleanup(func() {
		require.NoError(t, gatewaystore.UpdateGroupRatioByJSONString(originalRatios))
	})

	quotaSetting := gatewaystore.GetQuotaSetting()
	originalFreeModelPreconsume := quotaSetting.EnableFreeModelPreConsume
	quotaSetting.EnableFreeModelPreConsume = true
	t.Cleanup(func() {
		quotaSetting.EnableFreeModelPreConsume = originalFreeModelPreconsume
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	for _, testCase := range []struct {
		name     string
		model    string
		perCall  bool
		isTiered bool
	}{
		{name: "ratio", model: "gpt-4o"},
		{name: "per-call", model: "gpt-4o", perCall: true},
		{name: "tiered", model: "gpt-5.4", isTiered: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			info := &RelayInfo{
				OriginModelName: testCase.model,
				UsingGroup:      "free",
				UserGroup:       "default",
				UserSetting: dto.UserSetting{
					AcceptUnsetRatioModel: true,
				},
			}

			var priceData types.PriceData
			var err error
			if testCase.perCall {
				priceData, err = ModelPriceHelperPerCall(ctx, info)
			} else {
				priceData, err = ModelPriceHelper(ctx, info, 32, &types.TokenCountMeta{MaxTokens: 16})
			}

			require.NoError(t, err)
			require.True(t, priceData.FreeModel)
			require.Zero(t, priceData.QuotaToPreConsume)
			if testCase.isTiered {
				require.NotNil(t, info.TieredBillingSnapshot)
			}
		})
	}
}

func TestNonZeroGroupRatioStillPreconsumesTieredBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	quotaSetting := gatewaystore.GetQuotaSetting()
	originalFreeModelPreconsume := quotaSetting.EnableFreeModelPreConsume
	quotaSetting.EnableFreeModelPreConsume = true
	t.Cleanup(func() {
		quotaSetting.EnableFreeModelPreConsume = originalFreeModelPreconsume
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &RelayInfo{
		OriginModelName: "gpt-5.4",
		UsingGroup:      "default",
		UserGroup:       "default",
	}

	priceData, err := ModelPriceHelper(ctx, info, 32, &types.TokenCountMeta{MaxTokens: 16})

	require.NoError(t, err)
	require.False(t, priceData.FreeModel)
	require.Positive(t, priceData.QuotaToPreConsume)
}

func TestMarketplaceMultiplierOverridesMissingGlobalGroupRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRatios := gatewaystore.GroupRatio2JSONString()
	require.NoError(t, gatewaystore.UpdateGroupRatioByJSONString(`{"default":1}`))
	t.Cleanup(func() {
		require.NoError(t, gatewaystore.UpdateGroupRatioByJSONString(originalRatios))
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceGroupID, "market-group-1")
	httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceMultiplier, 0.37)

	ratio := HandleGroupRatio(ctx, &RelayInfo{UsingGroup: "market_dynamic_group", UserGroup: "default"})

	require.InDelta(t, 0.37, ratio.GroupRatio, 0.000001)
	require.False(t, ratio.HasSpecialRatio)
	require.Equal(t, -1.0, ratio.GroupSpecialRatio)
}

func TestMarketplaceChannelPriceAppliesOnlyFromCurrentRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceGroupID, "market-price-group")
	httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceMultiplier, 1.0)
	httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceModelPrices, map[string]marketplacedomain.ChannelModelPrice{
		"channel-only-model": {InputPricePerMillion: 2, OutputPricePerMillion: 8},
	})
	info := &RelayInfo{OriginModelName: "CHANNEL-ONLY-MODEL", UsingGroup: "market_dynamic", UserGroup: "default"}
	price, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{MaxTokens: 1000})
	require.NoError(t, err)
	require.False(t, price.UsePrice)
	require.InDelta(t, 1.0, price.ModelRatio, 0.000001)
	require.InDelta(t, 4.0, price.CompletionRatio, 0.000001)

	otherContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	otherContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	otherInfo := &RelayInfo{OriginModelName: "channel-only-model", UsingGroup: "default", UserGroup: "default"}
	_, err = ModelPriceHelper(otherContext, otherInfo, 1000, &types.TokenCountMeta{MaxTokens: 1000})
	require.ErrorContains(t, err, "价格")
}

func TestGlobalModelPriceTakesPriorityOverMarketplaceChannelPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRatios := gatewaystore.ModelRatio2JSONString()
	var ratios map[string]float64
	require.NoError(t, json.Unmarshal([]byte(originalRatios), &ratios))
	ratios["market-global-priority-model"] = 3
	updatedRatios, err := json.Marshal(ratios)
	require.NoError(t, err)
	require.NoError(t, gatewaystore.UpdateModelRatioByJSONString(string(updatedRatios)))
	t.Cleanup(func() { require.NoError(t, gatewaystore.UpdateModelRatioByJSONString(originalRatios)) })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceGroupID, "market-global-priority")
	httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceMultiplier, 1.0)
	httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceModelPrices, map[string]marketplacedomain.ChannelModelPrice{
		"market-global-priority-model": {InputPricePerMillion: 999, OutputPricePerMillion: 999},
	})
	info := &RelayInfo{OriginModelName: "market-global-priority-model", UsingGroup: "market_dynamic", UserGroup: "default"}
	price, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{MaxTokens: 1000})
	require.NoError(t, err)
	require.Equal(t, 3.0, price.ModelRatio)
}
