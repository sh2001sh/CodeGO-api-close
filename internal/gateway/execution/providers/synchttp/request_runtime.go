package synchttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sh2001sh/new-api/constant"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystream "github.com/sh2001sh/new-api/internal/gateway/stream"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformgeneral "github.com/sh2001sh/new-api/internal/platform/general"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	"github.com/sh2001sh/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type RequestAdaptor interface {
	GetRequestURL(info *relaycommon.RelayInfo) (string, error)
	SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error
}

func SetupAPIRequestHeader(info *relaycommon.RelayInfo, c *gin.Context, req *http.Header) {
	if info.RelayMode == gatewaycontract.RelayModeAudioTranscription || info.RelayMode == gatewaycontract.RelayModeAudioTranslation {
		return
	}
	if info.RelayMode == gatewaycontract.RelayModeRealtime {
		return
	}
	req.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	req.Set("Accept", c.Request.Header.Get("Accept"))
	if gatewaycontract.HasRemoteCompactionV2(c.Request.Header) {
		req.Set("X-Codex-Beta-Features", c.Request.Header.Get("X-Codex-Beta-Features"))
	}
	// Codex mints an opaque turn-state blob on a response and expects the
	// client to echo it on subsequent requests in the same turn. Preserve it
	// independently of generic header overrides so compaction/continuations
	// keep the upstream conversation state.
	if turnState := strings.TrimSpace(c.Request.Header.Get("x-codex-turn-state")); turnState != "" {
		req.Set("x-codex-turn-state", turnState)
	}
	if info.IsStream && c.Request.Header.Get("Accept") == "" {
		req.Set("Accept", "text/event-stream")
	}
}

func applyUpstreamContentLength(req *http.Request, info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	if info.UpstreamRequestBodySize > 0 && req.ContentLength <= 0 {
		req.ContentLength = info.UpstreamRequestBodySize
	}
}

func applyReplayableRequestBody(req *http.Request, body io.Reader) {
	if req == nil || req.GetBody != nil || body == nil {
		return
	}
	replayable, ok := body.(interface {
		NewReader() (io.ReadCloser, error)
	})
	if !ok {
		return
	}
	req.GetBody = replayable.NewReader
}

func startPingKeepAlive(c *gin.Context, pingInterval time.Duration) context.CancelFunc {
	pingerCtx, stopPinger := context.WithCancel(context.Background())

	gopool.Go(func() {
		defer func() {
			_ = recover()
		}()

		if pingInterval <= 0 {
			pingInterval = gatewaystream.DefaultPingInterval
		}

		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()

		var pingMutex sync.Mutex
		pingTimeout := time.NewTimer(120 * time.Minute)
		defer pingTimeout.Stop()

		for {
			select {
			case <-ticker.C:
				if err := sendPingData(c, &pingMutex); err != nil {
					return
				}
			case <-pingerCtx.Done():
				return
			case <-c.Request.Context().Done():
				return
			case <-pingTimeout.C:
				return
			}
		}
	})

	return stopPinger
}

func sendPingData(c *gin.Context, mutex *sync.Mutex) error {
	done := make(chan error, 1)
	go func() {
		mutex.Lock()
		defer mutex.Unlock()

		err := gatewaystream.PingData(c)
		if err != nil {
			logger.LogError(c, "SSE ping error: "+err.Error())
		}
		done <- err
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		return errors.New("SSE ping data send timeout")
	case <-c.Request.Context().Done():
		return errors.New("request context cancelled during ping")
	}
}

func DoRequest(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) (*http.Response, error) {
	var client *http.Client
	var err error
	markResponsesStreamRetrySafeBeforeConnect(c, info)
	responseHeaderTimeout := responseHeaderTimeoutForRequest(c, info)
	if info.ChannelSetting.Proxy != "" {
		client, err = platformhttpx.NewProxyHTTPClientWithResponseHeaderTimeout(info.ChannelSetting.Proxy, responseHeaderTimeout)
		if err != nil {
			return nil, fmt.Errorf("new proxy http client failed: %w", err)
		}
	} else {
		client = platformhttpx.GetHTTPClientWithResponseHeaderTimeout(responseHeaderTimeout)
	}

	var stopPinger context.CancelFunc
	if info.IsStream {
		gatewaystream.SetEventStreamHeaders(c)
		generalSettings := platformgeneral.GetSetting()
		if generalSettings.PingIntervalEnabled && !info.DisablePing {
			pingInterval := time.Duration(generalSettings.PingIntervalSeconds) * time.Second
			stopPinger = startPingKeepAlive(c, pingInterval)
			defer func() {
				if stopPinger != nil {
					stopPinger()
				}
			}()
		}
	}

	if info != nil && info.FirstByteTrace != nil {
		info.FirstByteTrace.MarkUpstreamRequestReady()
		req = relaycommon.WithOutboundHTTPTrace(req, info.FirstByteTrace, info.UpstreamRequestBodySize)
	}
	resp, err := client.Do(req)
	if err != nil {
		logger.LogError(c, "do request failed: "+err.Error())
		if isUpstreamResponseTimeout(err) {
			return nil, types.NewError(
				err,
				types.ErrorCodeChannelResponseTimeExceeded,
				types.ErrOptionWithStatusCode(http.StatusGatewayTimeout),
				types.ErrOptionWithHideErrMsg("upstream response timed out"),
			)
		}
		return nil, types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithHideErrMsg("upstream error: do request failed"))
	}
	if resp == nil {
		return nil, errors.New("resp is nil")
	}
	if info != nil && info.FirstByteTrace != nil {
		info.FirstByteTrace.MarkOutboundHTTPVersion(resp.ProtoMajor, resp.ProtoMinor)
		info.FirstByteTrace.MarkUpstreamResponseHeaders()
	}

	if upID := resp.Header.Get(constant.RequestIdKey); upID != "" {
		c.Set(constant.UpstreamRequestIdKey, upID)
	}

	if req.Body != nil {
		_ = req.Body.Close()
	}
	if c != nil && c.Request != nil && c.Request.Body != nil {
		_ = c.Request.Body.Close()
	}
	return resp, nil
}

func responseHeaderTimeoutForRequest(c *gin.Context, info *relaycommon.RelayInfo) time.Duration {
	baseTimeout := time.Duration(platformconfig.RelayResponseHeaderTimeout) * time.Second
	if info == nil {
		return baseTimeout
	}
	if info.RelayMode == gatewaycontract.RelayModeImagesGenerations ||
		info.RelayMode == gatewaycontract.RelayModeImagesEdits {
		imageTimeout := time.Duration(platformconfig.ImageResponseHeaderTimeout) * time.Second
		if imageTimeout <= 0 {
			return baseTimeout
		}
		return maxDuration(baseTimeout, imageTimeout)
	}
	// A long-lived Responses request may spend several minutes restoring
	// upstream conversation state before sending response headers. The short
	// first-attempt retry window is for ordinary requests only; applying it
	// here would terminate a healthy single route before stream idle/total
	// duration policies get a chance to observe progress.
	if info.IsStream && info.RelayMode == gatewaycontract.RelayModeResponses {
		if profile, found := relaycommon.RequestProfileFromContext(c); found &&
			(profile.RequestType == relaycommon.RequestTypeChatLongStream ||
				profile.RequestType == relaycommon.RequestTypeToolCallStream ||
				profile.HasConversationState) {
			if baseTimeout <= 0 {
				return 0
			}
			return baseTimeout
		}
		if relaycommon.IsLongContextRequest(c) || relaycommon.IsLongContextGPTRequest(info.OriginModelName, info.GetEstimatePromptTokens()) {
			if baseTimeout <= 0 {
				return 0
			}
			return baseTimeout
		}
	}
	if retryTimeout := relaycommon.RetryableResponsesAttemptTimeout(c); retryTimeout > 0 {
		return minPositiveDuration(baseTimeout, retryTimeout)
	}
	if baseTimeout <= 0 {
		return 0
	}
	if !relaycommon.IsLongContextGPTRequest(info.OriginModelName, info.GetEstimatePromptTokens()) {
		return baseTimeout
	}
	if info.GetEstimatePromptTokens() >= relaycommon.VeryLongContextPromptTokens {
		return maxDuration(baseTimeout, 90*time.Second)
	}
	return maxDuration(baseTimeout, 75*time.Second)
}

func minPositiveDuration(configured, fallback time.Duration) time.Duration {
	if configured <= 0 || fallback < configured {
		return fallback
	}
	return configured
}

func markResponsesStreamRetrySafeBeforeConnect(c *gin.Context, info *relaycommon.RelayInfo) {
	if c == nil || info == nil || !info.IsStream || info.RelayMode != gatewaycontract.RelayModeResponses || relaycommon.IsImageGenerationRequest(c) {
		return
	}
	c.Set(string(constant.ContextKeyResponsesStreamRetrySafe), true)
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

func isUpstreamResponseTimeout(err error) bool {
	if err == nil {
		return false
	}
	var timeoutErr interface{ Timeout() bool }
	if errors.As(err, &timeoutErr) && timeoutErr.Timeout() {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "timeout awaiting response headers") || strings.Contains(message, "response header timeout")
}

func DoAPIRequest(a RequestAdaptor, c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	fullRequestURL, err := a.GetRequestURL(info)
	if err != nil {
		return nil, fmt.Errorf("get request url failed: %w", err)
	}
	if platformconfig.DebugEnabled {
		println("fullRequestURL:", fullRequestURL)
	}
	req, err := http.NewRequestWithContext(requestContext(c), c.Request.Method, fullRequestURL, requestBody)
	if err != nil {
		return nil, fmt.Errorf("new request failed: %w", err)
	}
	applyUpstreamContentLength(req, info)
	applyReplayableRequestBody(req, requestBody)

	headers := req.Header
	if err := a.SetupRequestHeader(c, &headers, info); err != nil {
		return nil, fmt.Errorf("setup request header failed: %w", err)
	}

	headerOverride, err := resolveHeaderOverride(info, c)
	if err != nil {
		return nil, err
	}
	applyHeaderOverrideToRequest(req, headerOverride)

	resp, err := DoRequest(c, req, info)
	if err != nil {
		return nil, fmt.Errorf("do request failed: %w", err)
	}
	return resp, nil
}

// DoAPIRequestAt sends a follow-up request to an already resolved upstream URL
// while preserving the selected channel's authentication, proxy and overrides.
func DoAPIRequestAt(a RequestAdaptor, c *gin.Context, info *relaycommon.RelayInfo, method, target string, requestBody io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(c.Request.Context(), method, target, requestBody)
	if err != nil {
		return nil, fmt.Errorf("new follow-up request failed: %w", err)
	}
	headers := req.Header
	if err := a.SetupRequestHeader(c, &headers, info); err != nil {
		return nil, fmt.Errorf("setup follow-up request header failed: %w", err)
	}
	headerOverride, err := resolveHeaderOverride(info, c)
	if err != nil {
		return nil, err
	}
	applyHeaderOverrideToRequest(req, headerOverride)
	resp, err := DoRequest(c, req, info)
	if err != nil {
		return nil, fmt.Errorf("do follow-up request failed: %w", err)
	}
	return resp, nil
}

func DoFormAPIRequest(a RequestAdaptor, c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	fullRequestURL, err := a.GetRequestURL(info)
	if err != nil {
		return nil, fmt.Errorf("get request url failed: %w", err)
	}
	if platformconfig.DebugEnabled {
		println("fullRequestURL:", fullRequestURL)
	}
	req, err := http.NewRequestWithContext(requestContext(c), c.Request.Method, fullRequestURL, requestBody)
	if err != nil {
		return nil, fmt.Errorf("new request failed: %w", err)
	}
	applyUpstreamContentLength(req, info)
	applyReplayableRequestBody(req, requestBody)
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))

	headers := req.Header
	if err := a.SetupRequestHeader(c, &headers, info); err != nil {
		return nil, fmt.Errorf("setup request header failed: %w", err)
	}

	headerOverride, err := resolveHeaderOverride(info, c)
	if err != nil {
		return nil, err
	}
	applyHeaderOverrideToRequest(req, headerOverride)

	resp, err := DoRequest(c, req, info)
	if err != nil {
		return nil, fmt.Errorf("do request failed: %w", err)
	}
	return resp, nil
}

func DoWSSRequest(a RequestAdaptor, c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*websocket.Conn, error) {
	fullRequestURL, err := a.GetRequestURL(info)
	if err != nil {
		return nil, fmt.Errorf("get request url failed: %w", err)
	}

	targetHeader := http.Header{}
	if err := a.SetupRequestHeader(c, &targetHeader, info); err != nil {
		return nil, fmt.Errorf("setup request header failed: %w", err)
	}

	headerOverride, err := resolveHeaderOverride(info, c)
	if err != nil {
		return nil, err
	}
	for key, value := range headerOverride {
		targetHeader.Set(key, value)
	}
	targetHeader.Set("Content-Type", c.Request.Header.Get("Content-Type"))

	targetConn, _, err := websocket.DefaultDialer.DialContext(requestContext(c), fullRequestURL, targetHeader)
	if err != nil {
		return nil, fmt.Errorf("dial failed to %s: %w", fullRequestURL, err)
	}
	return targetConn, nil
}

// requestContext binds a synchronous upstream attempt to the incoming
// request.  Without this, net/http continues writing/reading an upstream
// request after the client has cancelled, leaving a charged reservation and a
// busy route slot alive until the upstream timeout.  Background Responses
// workers use their own request context and do not pass through this helper.
func requestContext(c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		return c.Request.Context()
	}
	return context.Background()
}
