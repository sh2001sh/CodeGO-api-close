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

func TestMarketplaceImagePriceOverridesGlobalPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalPrices := gatewaystore.ModelPrice2JSONString()
	var prices map[string]float64
	require.NoError(t, json.Unmarshal([]byte(originalPrices), &prices))
	prices["grok-imagine-image"] = 0.2
	updatedPrices, err := json.Marshal(prices)
	require.NoError(t, err)
	require.NoError(t, gatewaystore.UpdateModelPriceByJSONString(string(updatedPrices)))
	t.Cleanup(func() { require.NoError(t, gatewaystore.UpdateModelPriceByJSONString(originalPrices)) })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceGroupID, "market-grok-image")
	httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceMultiplier, 1.0)
	httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceModelPrices, map[string]marketplacedomain.ChannelModelPrice{
		"grok-imagine-image": {
			BillingMode:  marketplacedomain.ChannelBillingModePerCall,
			PricePerCall: 0.03,
		},
	})
	info := &RelayInfo{OriginModelName: "grok-imagine-image", UsingGroup: "market_dynamic", UserGroup: "default"}

	price, err := ModelPriceHelper(ctx, info, 0, &types.TokenCountMeta{})

	require.NoError(t, err)
	require.True(t, price.UsePrice)
	require.InDelta(t, 0.03, price.ModelPrice, 0.000001)
}

func TestMarketplaceImageWithoutChannelPriceIsRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceGroupID, "market-unpriced-image")
	httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceMultiplier, 1.0)
	httpctx.SetContextKey(ctx, constant.ContextKeyMarketplaceModelPrices, map[string]marketplacedomain.ChannelModelPrice{})
	info := &RelayInfo{
		OriginModelName: "grok-imagine-image-2.0",
		UsingGroup:      "market_dynamic",
		UserGroup:       "default",
		UserSetting:     dto.UserSetting{AcceptUnsetRatioModel: true},
	}

	_, err := ModelPriceHelper(ctx, info, 0, &types.TokenCountMeta{})

	require.ErrorContains(t, err, "未配置有效的按次价格")
}
