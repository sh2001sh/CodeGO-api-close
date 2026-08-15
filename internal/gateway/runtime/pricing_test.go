package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
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
