package http

import (
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
	"github.com/stretchr/testify/require"
)

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
