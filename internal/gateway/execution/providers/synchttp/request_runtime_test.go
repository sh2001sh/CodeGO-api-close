package synchttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/stretchr/testify/require"
)

type contextAwareRequestAdaptor struct {
	url string
}

func (a contextAwareRequestAdaptor) GetRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.url, nil
}

func (contextAwareRequestAdaptor) SetupRequestHeader(_ *gin.Context, _ *http.Header, _ *relaycommon.RelayInfo) error {
	return nil
}

func TestDoAPIRequestPropagatesClientCancellationToUpstream(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		started <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestCtx, cancel := context.WithCancel(context.Background())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestCtx)
	cancel()

	_, err := DoAPIRequest(contextAwareRequestAdaptor{url: server.URL}, ctx,
		&relaycommon.RelayInfo{RelayMode: gatewaycontract.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{}},
		strings.NewReader(`{"model":"gpt-5","input":"hello"}`),
	)
	require.Error(t, err)
	select {
	case <-started:
		t.Fatal("cancelled request reached upstream")
	default:
	}
}

func TestApplyReplayableRequestBodySetsIndependentGetBody(t *testing.T) {
	storage, err := platformhttpx.CreateBodyStorage([]byte("request-body"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	body := platformhttpx.ReaderOnly(storage)
	req, err := http.NewRequest(http.MethodPost, "https://example.com", body)
	require.NoError(t, err)

	applyReplayableRequestBody(req, body)
	require.NotNil(t, req.GetBody)

	prefix := make([]byte, 7)
	_, err = io.ReadFull(req.Body, prefix)
	require.NoError(t, err)
	replay, err := req.GetBody()
	require.NoError(t, err)
	t.Cleanup(func() { _ = replay.Close() })
	replayed, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.Equal(t, "request-body", string(replayed))
}

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
			require.Equal(t, testCase.expected, responseHeaderTimeoutForRequest(nil, info))
		})
	}
}

func TestResponseHeaderTimeoutCanBeDisabledForTextWithoutAffectingImages(t *testing.T) {
	previous := platformconfig.RelayResponseHeaderTimeout
	previousImage := platformconfig.ImageResponseHeaderTimeout
	t.Cleanup(func() {
		platformconfig.RelayResponseHeaderTimeout = previous
		platformconfig.ImageResponseHeaderTimeout = previousImage
	})

	platformconfig.RelayResponseHeaderTimeout = 0
	platformconfig.ImageResponseHeaderTimeout = 120

	text := &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol", RelayMode: gatewaycontract.RelayModeResponses}
	image := &relaycommon.RelayInfo{OriginModelName: "gpt-image-2", RelayMode: gatewaycontract.RelayModeImagesGenerations}
	require.Zero(t, responseHeaderTimeoutForRequest(nil, text))
	require.Equal(t, 120*time.Second, responseHeaderTimeoutForRequest(nil, image))
}

func TestRetryableResponsesFirstAttemptBoundsResponseHeaderWait(t *testing.T) {
	previous := platformconfig.RelayResponseHeaderTimeout
	platformconfig.RelayResponseHeaderTimeout = 0
	t.Cleanup(func() { platformconfig.RelayResponseHeaderTimeout = previous })

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	profile := relaycommon.InitializeRequestProfile(
		context,
		"gpt-5.6-sol",
		context.Request.URL.Path,
		relaycommon.RequestProfileHint{IsStream: true},
	)
	budget := relaycommon.StartRequestBudget(context, profile, time.Now())
	require.True(t, budget.TryBeginAttempt(time.Now(), "provider:a"))
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol", RelayMode: gatewaycontract.RelayModeResponses, IsStream: true}

	markResponsesStreamRetrySafeBeforeConnect(context, info)
	require.True(t, context.GetBool(string(constant.ContextKeyResponsesStreamRetrySafe)))
	require.Equal(t, 30*time.Second, responseHeaderTimeoutForRequest(context, info))

	require.True(t, budget.TryBeginAttempt(time.Now(), "provider:a"))
	require.Zero(t, responseHeaderTimeoutForRequest(context, info))
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

func TestSetupAPIRequestHeaderForwardsCodexTurnState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("x-codex-turn-state", "opaque-turn-state")

	headers := http.Header{}
	SetupAPIRequestHeader(&relaycommon.RelayInfo{RelayMode: gatewaycontract.RelayModeResponses}, ctx, &headers)

	require.Equal(t, "opaque-turn-state", headers.Get("x-codex-turn-state"))
}
