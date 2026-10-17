package app

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	"github.com/stretchr/testify/require"
)

func TestCreditPackChannelContextRefreshesSupportOnRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &gatewayruntime.RelayInfo{}
	for _, supported := range []bool{true, false} {
		err := SetupContextForSelectedChannel(context, &gatewayschema.Channel{
			Id: 10, Type: 1, Key: "test-key", ChannelScope: gatewayschema.ChannelScopeExternal,
			CreditPackSupported: supported,
		}, "gpt-5")
		require.Nil(t, err)
		info.InitChannelMeta(context)
		require.Equal(t, supported, info.ChannelMeta.CreditPackSupported)
	}
}

func TestOfficialCreditPackDefaultAndExternalRetryIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &gatewayruntime.RelayInfo{}
	for _, test := range []struct {
		scope         string
		enabled, want bool
	}{
		{gatewayschema.ChannelScopeOfficial, false, true},
		{gatewayschema.ChannelScopeExternal, false, false},
		{gatewayschema.ChannelScopeExternal, true, true},
		{gatewayschema.ChannelScopeOfficial, false, true},
	} {
		require.Nil(t, SetupContextForSelectedChannel(context, &gatewayschema.Channel{
			Id: 10, Type: 1, Key: "test-key", ChannelScope: test.scope, CreditPackSupported: test.enabled,
		}, "gpt-5"))
		info.InitChannelMeta(context)
		require.Equal(t, test.want, info.ChannelMeta.CreditPackSupported)
	}
}
