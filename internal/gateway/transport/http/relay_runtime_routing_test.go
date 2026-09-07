package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestAutoRetryStopsWhenIncomingContextIsCancelled(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestCtx, cancel := context.WithCancel(context.Background())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestCtx)
	gatewayruntime.MarkAutoRouteRequest(ctx)
	gatewayruntime.MarkRemainingCrossGroupRoutes(ctx, 3)
	cancel()
	err := types.NewOpenAIError(context.Canceled, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	require.False(t, shouldRetry(ctx, err, 3), "cancelled ingress cannot make any fallback request succeed")
}

func TestSingleBindingAutoUsesNormalGroupRecovery(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	httpctx.SetContextKey(ctx, constant.ContextKeyUnifiedAutoBindings, []marketplaceapp.RoutingBinding{{InternalGroup: "only"}})
	httpctx.SetContextKey(ctx, constant.ContextKeyUnifiedAutoIndex, 0)
	httpctx.SetContextKey(ctx, constant.ContextKeyRetryFallbackChannelID, 163)
	retry := 1
	_, handled, err := nextUnifiedAutoChannel(ctx, &gatewayruntime.RelayInfo{OriginModelName: "gpt-5.6-sol"}, &gatewayroutingapp.RetryParam{Ctx: ctx, TokenGroup: "only", Retry: &retry})
	require.False(t, handled, "a single-group pool must reach the same in-group recovery as direct group requests")
	require.Nil(t, err)
}

func TestExhaustedAutoPreservesLastUpstreamError(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	httpctx.SetContextKey(ctx, constant.ContextKeyUnifiedAutoBindings, []marketplaceapp.RoutingBinding{{InternalGroup: "first"}, {InternalGroup: "last"}})
	httpctx.SetContextKey(ctx, constant.ContextKeyUnifiedAutoIndex, 1)
	upstreamErr := types.NewOpenAIError(errors.New("upstream unavailable"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	retry := 2
	_, handled, err := nextUnifiedAutoChannel(ctx, &gatewayruntime.RelayInfo{OriginModelName: "gpt-5.6-sol", LastError: upstreamErr}, &gatewayroutingapp.RetryParam{Ctx: ctx, Retry: &retry})
	require.True(t, handled)
	require.Same(t, upstreamErr, err, "selection exhaustion must not hide the original upstream failure")
}

func TestAutoDoesNotRetryContentPolicyRefusal(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	gatewayruntime.MarkAutoRouteRequest(ctx)
	gatewayruntime.MarkRemainingCrossGroupRoutes(ctx, 2)
	ctx.Set(string(constant.ContextKeyResponsesStreamRetrySafe), true)
	err := types.NewOpenAIError(errors.New("request blocked by our safety systems"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	require.False(t, shouldRetry(ctx, err, 2))
}

func TestAutoRetriesDisabledUpstreamGroupOnlyBeforeOutput(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	gatewayruntime.MarkAutoRouteRequest(ctx)
	gatewayruntime.MarkRemainingCrossGroupRoutes(ctx, 2)
	err := types.NewOpenAIError(errors.New("API Key 所属分组已停用"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)
	require.True(t, shouldRetry(ctx, err, 2))
	ctx.Set(string(constant.ContextKeyStreamContentDelivered), true)
	require.False(t, shouldRetry(ctx, err, 2))
}

func TestNextUnifiedAutoChannelMovesToFollowingBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	bindings := []marketplaceapp.RoutingBinding{
		{RouteKey: "primary", InternalGroup: "primary", SourceType: marketplacedomain.SourceTypeOfficial},
		{RouteKey: "fallback", InternalGroup: "fallback", SourceType: marketplacedomain.SourceTypeOfficial},
		{RouteKey: "reserve", InternalGroup: "reserve", SourceType: marketplacedomain.SourceTypeOfficial},
	}
	httpctx.SetContextKey(context, constant.ContextKeyUnifiedAutoBindings, bindings)
	httpctx.SetContextKey(context, constant.ContextKeyUnifiedAutoIndex, 0)

	originalSelector := selectUnifiedAutoRetryChannel
	selectUnifiedAutoRetryChannel = func(param *gatewayroutingapp.RetryParam) (*gatewayschema.Channel, string, error) {
		require.Equal(t, "fallback", param.TokenGroup)
		baseURL := "https://fallback.example/v1"
		return &gatewayschema.Channel{Id: 34, Type: constant.ChannelTypeOpenAI, Key: "test-key", BaseURL: &baseURL}, param.TokenGroup, nil
	}
	t.Cleanup(func() { selectUnifiedAutoRetryChannel = originalSelector })

	retry := 1
	retryParam := &gatewayroutingapp.RetryParam{Ctx: context, TokenGroup: "primary", ModelName: "gpt-5.6-sol", Retry: &retry}
	info := &gatewayruntime.RelayInfo{OriginModelName: "gpt-5.6-sol"}
	channel, handled, apiErr := nextUnifiedAutoChannel(context, info, retryParam)

	require.True(t, handled)
	require.Nil(t, apiErr)
	require.NotNil(t, channel)
	require.Equal(t, 34, channel.Id)
	require.Equal(t, "fallback", retryParam.TokenGroup)
	require.Equal(t, "fallback", httpctx.GetContextKeyString(context, constant.ContextKeyUsingGroup))
	require.Equal(t, 1, httpctx.GetContextKeyInt(context, constant.ContextKeyUnifiedAutoIndex))
	require.True(t, gatewayruntime.HasRemainingCrossGroupRoute(context))
}

func TestUnifiedAutoExhaustsHealthyGroupsBeforeRecoveryProbe(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	bindings := []marketplaceapp.RoutingBinding{{InternalGroup: "used"}, {InternalGroup: "cooling"}, {InternalGroup: "healthy"}}
	httpctx.SetContextKey(ctx, constant.ContextKeyUnifiedAutoBindings, bindings)
	httpctx.SetContextKey(ctx, constant.ContextKeyUnifiedAutoIndex, 0)
	previous := selectUnifiedAutoRetryChannel
	t.Cleanup(func() { selectUnifiedAutoRetryChannel = previous })
	visited := []string{}
	selectUnifiedAutoRetryChannel = func(param *gatewayroutingapp.RetryParam) (*gatewayschema.Channel, string, error) {
		visited = append(visited, param.TokenGroup)
		require.True(t, param.HealthyOnly, "a healthy reserve exists, so never probe the earlier cooling group")
		if param.TokenGroup == "cooling" {
			return nil, param.TokenGroup, nil
		}
		baseURL := "https://healthy.example"
		return &gatewayschema.Channel{Id: 354, Type: constant.ChannelTypeOpenAI, Key: "test", BaseURL: &baseURL}, param.TokenGroup, nil
	}
	retry := 1
	channel, handled, err := nextUnifiedAutoChannel(ctx, &gatewayruntime.RelayInfo{OriginModelName: "gpt-5.6-sol"}, &gatewayroutingapp.RetryParam{Ctx: ctx, Retry: &retry})
	require.True(t, handled)
	require.Nil(t, err)
	require.Equal(t, 354, channel.Id)
	require.Equal(t, []string{"cooling", "healthy"}, visited)
	require.True(t, gatewayruntime.HasRemainingCrossGroupRoute(ctx), "skipped recovery route must remain available")
	selectUnifiedAutoRetryChannel = func(param *gatewayroutingapp.RetryParam) (*gatewayschema.Channel, string, error) {
		require.Equal(t, "cooling", param.TokenGroup)
		if param.HealthyOnly {
			return nil, param.TokenGroup, nil
		}
		return &gatewayschema.Channel{Id: 355, Type: constant.ChannelTypeOpenAI, Key: "test"}, param.TokenGroup, nil
	}
	channel, handled, err = nextUnifiedAutoChannel(ctx, &gatewayruntime.RelayInfo{OriginModelName: "gpt-5.6-sol"}, &gatewayroutingapp.RetryParam{Ctx: ctx, Retry: &retry})
	require.True(t, handled)
	require.Nil(t, err)
	require.Equal(t, 355, channel.Id)
	require.False(t, gatewayruntime.HasRemainingCrossGroupRoute(ctx))
}
