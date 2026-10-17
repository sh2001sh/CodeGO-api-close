package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	responsesws "github.com/sh2001sh/new-api/internal/gateway/responsesws"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceAdaptorNativeFailureCannotFallbackToPrivateHTTP(t *testing.T) {
	var reached atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	session := responsesws.NewSession()
	defer session.Close()
	require.NoError(t, session.BindRoute(1, 0, true))
	responsesws.Attach(c, session)
	info := &relaycommon.RelayInfo{
		RelayMode: gatewaycontract.RelayModeResponses, IsStream: true, RequestURLPath: "/v1/responses", RelayFormat: types.RelayFormatOpenAIResponses,
		MarketplaceSourceType: "marketplace_user", MarketplaceOwnerID: 7, MarketplaceGroupID: "group-7",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ChannelBaseUrl: server.URL, ApiKey: "isolated-credential"},
	}
	_, err := (&Adaptor{ChannelType: constant.ChannelTypeOpenAI}).DoRequest(c, info, strings.NewReader(`{"model":"gpt-5","stream":true}`))
	require.Error(t, err)
	require.Zero(t, reached.Load(), "neither the native handshake nor its HTTP fallback may access the private target")
}
