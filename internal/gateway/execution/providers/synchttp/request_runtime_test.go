package synchttp

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/stretchr/testify/require"
)

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout awaiting response headers" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestIsUpstreamResponseTimeout(t *testing.T) {
	require.True(t, isUpstreamResponseTimeout(timeoutError{}))
	require.True(t, isUpstreamResponseTimeout(fmt.Errorf("wrapped: %w", timeoutError{})))
	require.True(t, isUpstreamResponseTimeout(errors.New("net/http: timeout awaiting response headers")))
	require.False(t, isUpstreamResponseTimeout(&net.DNSError{IsTimeout: false}))
	require.False(t, isUpstreamResponseTimeout(errors.New("upstream returned bad gateway")))
}

func TestSetupAPIRequestHeaderForwardsRemoteCompactionFeature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("X-Codex-Beta-Features", "foo, remote_compaction_v2")

	headers := http.Header{}
	SetupAPIRequestHeader(&relaycommon.RelayInfo{RelayMode: gatewaycontract.RelayModeResponses}, ctx, &headers)

	require.Equal(t, "foo, remote_compaction_v2", headers.Get("X-Codex-Beta-Features"))
}
