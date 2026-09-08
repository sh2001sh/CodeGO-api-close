package http

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	responsesws "github.com/sh2001sh/new-api/internal/gateway/responsesws"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/stretchr/testify/require"
)

func TestWebsocketContinuationPreservesFinalMarketplaceBillingContext(t *testing.T) {
	parent, _ := gin.CreateTestContext(httptest.NewRecorder())
	parent.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	httpctx.SetContextKey(parent, constant.ContextKeyUsingGroup, "market:auto")
	httpctx.SetContextKey(parent, constant.ContextKeyTokenGroup, "market:pool:pool-1")
	session := responsesws.NewSession()
	defer session.Close()
	first := newResponsesWebsocketTurnContext(parent, httptest.NewRecorder(), context.Background(), session, []byte(`{"model":"gpt-6-astra"}`))
	httpctx.SetContextKey(first, constant.ContextKeyUsingGroup, "final-market-group")
	httpctx.SetContextKey(first, constant.ContextKeyTokenGroup, "final-market-group")
	httpctx.SetContextKey(first, constant.ContextKeyMarketplaceGroupID, "group-163")
	httpctx.SetContextKey(first, constant.ContextKeyMarketplaceOwnerID, 42)
	httpctx.SetContextKey(first, constant.ContextKeyMarketplaceSourceType, "marketplace_user")
	httpctx.SetContextKey(first, constant.ContextKeyMarketplaceCreditPolicy, "subscription_and_universal")
	httpctx.SetContextKey(first, constant.ContextKeyMarketplaceMultiplier, 0.16)
	httpctx.SetContextKey(first, constant.ContextKeyChannelId, 163)
	prices := map[string]marketplaceapp.ChannelModelPrice{"gpt-6-astra": {InputPricePerMillion: 2}}
	httpctx.SetContextKey(first, constant.ContextKeyMarketplaceModelPrices, prices)
	first.Set(gatewayruntime.RoutePoolNameContextKey, "My Pool")
	first.Set("billing_session", "previous-reservation")
	gatewayruntime.MarkAutoRouteRequest(first)
	require.NoError(t, session.BindRoute(163, 0, false))
	saveResponsesWebsocketRoutingContext(parent, first, session)
	delete(prices, "gpt-6-astra")

	second := newResponsesWebsocketTurnContext(parent, httptest.NewRecorder(), context.Background(), session, []byte(`{"model":"gpt-6-astra"}`))
	info := gatewayruntime.GenRelayInfoResponses(second, &dto.OpenAIResponsesRequest{Model: "gpt-6-astra"})
	require.Equal(t, "final-market-group", info.UsingGroup)
	require.Equal(t, "group-163", info.MarketplaceGroupID)
	require.Equal(t, "subscription_and_universal", info.MarketplaceCreditPolicy)
	require.Equal(t, 42, info.MarketplaceOwnerID)
	require.Equal(t, 0.16, info.MarketplaceMultiplier)
	require.NotEqual(t, first.GetString(constant.RequestIdKey), info.RequestId)
	require.Empty(t, second.GetString("billing_session"))
	require.Empty(t, info.BillingSource)
	require.Equal(t, "market:auto", parent.GetString(string(constant.ContextKeyUsingGroup)))
	restored, found := httpctx.GetContextKeyType[map[string]marketplaceapp.ChannelModelPrice](second, constant.ContextKeyMarketplaceModelPrices)
	require.True(t, found)
	require.Contains(t, restored, "gpt-6-astra")
	gatewayruntime.StartRouteDecision(second, "gpt-6-astra", info.UsingGroup)
	require.NoError(t, bindResponsesWebsocketRoute(second))
	other := map[string]interface{}{}
	gatewayruntime.AttachRouteLogInfo(second, other)
	require.Equal(t, "My Pool", other["route_pool_name"])
	require.Equal(t, "final-market-group", other["actual_group"])
	require.Equal(t, 1, other["route_summary"].(gatewayruntime.RouteLogSummary).SelectedOrder)
	saveResponsesWebsocketRoutingContext(parent, second, session)
	afterRejected := newResponsesWebsocketTurnContext(parent, httptest.NewRecorder(), context.Background(), session, []byte(`{"model":"gpt-6-astra"}`))
	require.Equal(t, "group-163", afterRejected.GetString(string(constant.ContextKeyMarketplaceGroupID)), "a turn rejected before channel setup must not erase the pinned billing context")

	session.ResetRoute()
	third := newResponsesWebsocketTurnContext(parent, httptest.NewRecorder(), context.Background(), session, []byte(`{"model":"gpt-6-astra"}`))
	require.Empty(t, third.GetString(string(constant.ContextKeyMarketplaceGroupID)))
	require.Equal(t, "market:auto", third.GetString(string(constant.ContextKeyUsingGroup)))
	_, pinned := httpctx.GetContextKey(third, constant.ContextKeyTokenSpecificChannelId)
	require.False(t, pinned)
}
