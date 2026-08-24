package openai

import (
	"io"
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

func TestResponsesWebSocketHandshakeFailureFallsBackToHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var websocketRequests atomic.Int32
	var httpRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.EqualFold(request.Header.Get("Upgrade"), "websocket") {
			websocketRequests.Add(1)
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		httpRequests.Add(1)
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read HTTP fallback body: %v", err)
			return
		}
		if string(body) != `{"model":"gpt-5","stream":true}` {
			t.Errorf("unexpected HTTP fallback body: %s", body)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"resp_http","status":"completed"}`))
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	session := responsesws.NewSession()
	require.NoError(t, session.BindRoute(1, 0, true))
	responsesws.Attach(c, session)

	info := &relaycommon.RelayInfo{
		RelayMode:      gatewaycontract.RelayModeResponses,
		IsStream:       true,
		RequestURLPath: "/v1/responses",
		RelayFormat:    types.RelayFormatOpenAIResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelBaseUrl: server.URL,
			ApiKey:         "test-key",
		},
	}
	response, err := (&Adaptor{ChannelType: constant.ChannelTypeOpenAI}).DoRequest(
		c,
		info,
		strings.NewReader(`{"model":"gpt-5","stream":true}`),
	)
	require.NoError(t, err)
	httpResponse, ok := response.(*http.Response)
	require.True(t, ok)
	defer httpResponse.Body.Close()
	require.Equal(t, http.StatusOK, httpResponse.StatusCode)
	require.EqualValues(t, 1, websocketRequests.Load())
	require.EqualValues(t, 1, httpRequests.Load())
	require.False(t, session.NativeEnabled())
}
