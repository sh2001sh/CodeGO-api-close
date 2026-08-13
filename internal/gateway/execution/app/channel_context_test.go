package app

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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
