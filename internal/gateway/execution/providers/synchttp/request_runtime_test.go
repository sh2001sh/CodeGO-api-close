package synchttp

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
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

func TestResponseHeaderTimeoutForRequest(t *testing.T) {
	previous := platformconfig.RelayResponseHeaderTimeout
	previousImage := platformconfig.ImageResponseHeaderTimeout
	platformconfig.RelayResponseHeaderTimeout = 45
	platformconfig.ImageResponseHeaderTimeout = 120
	t.Cleanup(func() {
		platformconfig.RelayResponseHeaderTimeout = previous
		platformconfig.ImageResponseHeaderTimeout = previousImage
	})

	testCases := []struct {
		name         string
		model        string
		promptTokens int
		expected     time.Duration
		relayMode    int
	}{
		{name: "short gpt request uses shared timeout", model: "gpt-5.6-sol", promptTokens: 99_999, expected: 45 * time.Second},
		{name: "long gpt request", model: "gpt-5.6-sol", promptTokens: 100_000, expected: 75 * time.Second},
		{name: "very long gpt request", model: "gpt-5.6-sol", promptTokens: 200_000, expected: 90 * time.Second},
		{name: "non gpt request", model: "claude-opus", promptTokens: 200_000, expected: 45 * time.Second},
		{name: "image generation uses image timeout", model: "gpt-image-2", expected: 120 * time.Second, relayMode: gatewaycontract.RelayModeImagesGenerations},
		{name: "image edit uses image timeout", model: "gpt-image-2", expected: 120 * time.Second, relayMode: gatewaycontract.RelayModeImagesEdits},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{OriginModelName: testCase.model, RelayMode: testCase.relayMode}
			info.SetEstimatePromptTokens(testCase.promptTokens)
			require.Equal(t, testCase.expected, responseHeaderTimeoutForRequest(info))
		})
	}
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
