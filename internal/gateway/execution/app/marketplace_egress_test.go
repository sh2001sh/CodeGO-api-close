package app

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	"github.com/stretchr/testify/require"
)

func TestSelectedMarketplaceChannelEstablishesEgressWithoutBillingBinding(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	baseURL := "https://example.com"
	channel := &gatewayschema.Channel{Id: 99001, Type: 1, Key: "isolated-credential", BaseURL: &baseURL, ChannelScope: gatewayschema.ChannelScopeExternal,
		OtherInfo: `{"marketplace_channel_id":"owner-channel","marketplace_group_id":"owner-group","source_type":"marketplace_user"}`}
	require.Nil(t, SetupContextForSelectedChannel(c, channel, "gpt-test"))
	info := gatewayruntime.GenRelayInfoOpenAI(c, nil)
	info.InitChannelMeta(c)
	require.True(t, info.UsesMarketplaceEgress())
	require.Empty(t, info.MarketplaceGroupID)
	require.Zero(t, info.MarketplaceOwnerID)
	require.Empty(t, info.MarketplaceSourceType)
	// Reusing the same context for an administrator-owned upstream must clear
	// this egress flag rather than retain the previous selected owner's policy.
	channel.OtherInfo = ""
	require.Nil(t, SetupContextForSelectedChannel(c, channel, "gpt-test"))
	info = gatewayruntime.GenRelayInfoOpenAI(c, nil)
	info.InitChannelMeta(c)
	require.False(t, info.UsesMarketplaceEgress())
}
