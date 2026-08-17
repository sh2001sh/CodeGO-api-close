package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/stretchr/testify/require"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
)

func TestSetupContextForSelectedChannelRefreshesFaultDomainOnRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	firstBaseURL := "https://first.example/v1"
	secondBaseURL := "https://second.example/v1"

	firstError := SetupContextForSelectedChannel(context, &gatewayschema.Channel{
		Id: 1, Type: 1, Key: "first-key", BaseURL: &firstBaseURL, ChannelInfo: gatewayschema.ChannelInfo{},
	}, "gpt-test")
	require.Nil(t, firstError, "%+v", firstError)
	require.Equal(t, "1:first.example", context.GetString("channel_fault_domain"))

	secondError := SetupContextForSelectedChannel(context, &gatewayschema.Channel{
		Id: 2, Type: 1, Key: "second-key", BaseURL: &secondBaseURL, ChannelInfo: gatewayschema.ChannelInfo{},
	}, "gpt-test")
	require.Nil(t, secondError, "%+v", secondError)
	require.Equal(t, "1:second.example", context.GetString("channel_fault_domain"))
}

func TestSetupContextForSelectedChannelKeepsExplicitPoolFaultDomain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	baseURL := "https://proxy.example/v1"
	context.Set("automatic_route_pool_fault_domain", "provider:primary")

	err := SetupContextForSelectedChannel(context, &gatewayschema.Channel{
		Id: 1, Type: 1, Key: "channel-key", BaseURL: &baseURL, ChannelInfo: gatewayschema.ChannelInfo{},
	}, "gpt-test")
	require.Nil(t, err, "%+v", err)
	require.Equal(t, "provider:primary", context.GetString("channel_fault_domain"))
}

func TestSetupContextForSelectedChannelRotatesMultiKeyOnRetry(t *testing.T) {
	previousCacheEnabled := platformconfig.MemoryCacheEnabled
	platformconfig.MemoryCacheEnabled = false
	t.Cleanup(func() { platformconfig.MemoryCacheEnabled = previousCacheEnabled })

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	baseURL := "https://multi-key.example/v1"
	channel := &gatewayschema.Channel{
		Id:      6,
		Type:    1,
		BaseURL: &baseURL,
		Keys:    []string{"key-0", "key-1", "key-2"},
		ChannelInfo: gatewayschema.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 3,
			MultiKeyMode: constant.MultiKeyModeRandom,
		},
	}

	firstErr := SetupContextForSelectedChannel(context, channel, "gpt-5.6-sol")
	require.Nil(t, firstErr, "%+v", firstErr)
	firstIndex := httpctx.GetContextKeyInt(context, constant.ContextKeyChannelMultiKeyIndex)

	secondErr := SetupContextForSelectedChannel(context, channel, "gpt-5.6-sol")
	require.Nil(t, secondErr, "%+v", secondErr)
	secondIndex := httpctx.GetContextKeyInt(context, constant.ContextKeyChannelMultiKeyIndex)

	require.NotEqual(t, firstIndex, secondIndex)
}

func TestSetupContextForSingleChannelReusesMultiKeyIndex(t *testing.T) {
	previousCacheEnabled := platformconfig.MemoryCacheEnabled
	platformconfig.MemoryCacheEnabled = false
	t.Cleanup(func() { platformconfig.MemoryCacheEnabled = previousCacheEnabled })

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	gatewayruntime.InitializeRequestProfile(context, "gpt-5.6-sol", context.Request.URL.Path, gatewayruntime.RequestProfileHint{IsStream: true})
	httpctx.SetContextKey(context, constant.ContextKeyUsingGroup, "plus-only-route")
	baseURL := "https://multi-key.example/v1"
	channel := &gatewayschema.Channel{
		Id: 6, Type: 1, BaseURL: &baseURL, Keys: []string{"key-0", "key-1", "key-2"},
		ChannelInfo: gatewayschema.ChannelInfo{IsMultiKey: true, MultiKeySize: 3, MultiKeyMode: constant.MultiKeyModeRandom},
	}

	originalHasAlternative := hasAlternativeSelectableRoute
	hasAlternativeSelectableRoute = func(channelID int, group, model string) (bool, error) {
		require.Equal(t, 6, channelID)
		require.Equal(t, "plus-only-route", group)
		require.Equal(t, "gpt-5.6-sol", model)
		return false, nil
	}
	t.Cleanup(func() { hasAlternativeSelectableRoute = originalHasAlternative })

	firstErr := SetupContextForSelectedChannel(context, channel, "gpt-5.6-sol")
	require.Nil(t, firstErr, "%+v", firstErr)
	firstIndex := httpctx.GetContextKeyInt(context, constant.ContextKeyChannelMultiKeyIndex)
	require.True(t, gatewayruntime.IsSingleChannelRoute(context))

	secondErr := SetupContextForSelectedChannel(context, channel, "gpt-5.6-sol")
	require.Nil(t, secondErr, "%+v", secondErr)
	secondIndex := httpctx.GetContextKeyInt(context, constant.ContextKeyChannelMultiKeyIndex)

	require.Equal(t, firstIndex, secondIndex)
}
