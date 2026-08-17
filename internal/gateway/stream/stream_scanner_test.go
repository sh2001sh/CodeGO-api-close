package stream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformgeneral "github.com/sh2001sh/new-api/internal/platform/general"
	"github.com/stretchr/testify/require"
)

type closeTrackingReader struct {
	closed bool
}

func (r *closeTrackingReader) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (r *closeTrackingReader) Close() error {
	r.closed = true
	return nil
}

func TestCloseTimedOutStreamClosesResponseBody(t *testing.T) {
	body := &closeTrackingReader{}
	closeTimedOutStream(&http.Response{Body: body})
	require.True(t, body.closed)
}

func TestIsClientGoneMarksRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := httptest.NewRequest(http.MethodPost, "http://example.test/v1/responses", nil)
	requestContext, cancel := contextWithCancel(request.Context())
	request = request.WithContext(requestContext)
	context.Request = request
	cancel()

	require.True(t, IsClientGone(context))
	require.True(t, context.GetBool(string(constant.ContextKeyClientGone)))
}

func contextWithCancel(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}

type blockingReader struct {
	closed chan struct{}
}

func (r *blockingReader) Read(_ []byte) (int, error) {
	<-r.closed
	return 0, io.EOF
}

func (r *blockingReader) Close() error {
	select {
	case <-r.closed:
	default:
		close(r.closed)
	}
	return nil
}

func TestScanResponseMarksMaximumDurationSeparatelyFromIdleTimeout(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	oldFirstByteTimeout := constant.StreamingFirstByteTimeout
	oldMaxDuration := constant.StreamingMaxDuration
	constant.StreamingTimeout = 5
	constant.StreamingFirstByteTimeout = 0
	constant.StreamingMaxDuration = 1
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
		constant.StreamingFirstByteTimeout = oldFirstByteTimeout
		constant.StreamingMaxDuration = oldMaxDuration
	})

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request, _ = http.NewRequest(http.MethodPost, "http://example.test/v1/responses", nil)
	body := &blockingReader{closed: make(chan struct{})}
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol"}

	started := time.Now()
	ScanResponse(context, &http.Response{Body: body}, info, func(string, *Result) {})

	require.Less(t, time.Since(started), 3*time.Second)
	require.Equal(t, gatewaycontract.StreamEndReasonMaxDuration, info.StreamStatus.EndReason)
	require.True(t, relaycommon.IsLocalStreamMaxDurationExceeded(context))
}

func TestScanResponseEndsAdaptiveLongContextWhenSemanticProgressStops(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	oldFirstByteTimeout := constant.StreamingFirstByteTimeout
	oldMaxDuration := constant.StreamingLongContextMaxDuration
	oldProgress := constant.StreamingAdaptiveProgressTimeout
	oldInitial := constant.StreamingAdaptiveInitialTimeout
	constant.StreamingTimeout = 5
	constant.StreamingFirstByteTimeout = 0
	constant.StreamingLongContextMaxDuration = 10
	constant.StreamingAdaptiveProgressTimeout = 1
	constant.StreamingAdaptiveInitialTimeout = 1
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
		constant.StreamingFirstByteTimeout = oldFirstByteTimeout
		constant.StreamingLongContextMaxDuration = oldMaxDuration
		constant.StreamingAdaptiveProgressTimeout = oldProgress
		constant.StreamingAdaptiveInitialTimeout = oldInitial
	})

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request, _ = http.NewRequest(http.MethodPost, "http://example.test/v1/responses", nil)
	relaycommon.MarkLongContextRequest(context, "gpt-5.6-sol", relaycommon.LongContextPromptTokenThreshold)
	body := &blockingReader{closed: make(chan struct{})}
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol"}

	started := time.Now()
	ScanResponse(context, &http.Response{Body: body}, info, func(string, *Result) {})

	require.Less(t, time.Since(started), 3*time.Second)
	require.Equal(t, gatewaycontract.StreamEndReasonTimeout, info.StreamStatus.EndReason)
	require.False(t, relaycommon.IsLocalStreamMaxDurationExceeded(context))
	require.Equal(t, relaycommon.LocalStreamTimeoutAdaptiveInitial, relaycommon.LocalStreamTimeoutReason(context))
}

func TestScanResponseSingleChannelUsesAbsoluteMaxInsteadOfInitialDeadline(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	oldMaxDuration := constant.StreamingLongContextMaxDuration
	oldProgress := constant.StreamingAdaptiveProgressTimeout
	oldInitial := constant.StreamingAdaptiveInitialTimeout
	constant.StreamingTimeout = 5
	constant.StreamingLongContextMaxDuration = 1
	constant.StreamingAdaptiveProgressTimeout = 1
	constant.StreamingAdaptiveInitialTimeout = 1
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
		constant.StreamingLongContextMaxDuration = oldMaxDuration
		constant.StreamingAdaptiveProgressTimeout = oldProgress
		constant.StreamingAdaptiveInitialTimeout = oldInitial
	})

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	relaycommon.MarkLongContextRequest(context, "gpt-5.6-sol", relaycommon.LongContextPromptTokenThreshold)
	relaycommon.MarkSingleChannelRoute(context, true)
	body := &blockingReader{closed: make(chan struct{})}
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol"}

	ScanResponse(context, &http.Response{Body: body}, info, func(string, *Result) {})

	require.Equal(t, gatewaycontract.StreamEndReasonMaxDuration, info.StreamStatus.EndReason)
	require.Equal(t, relaycommon.LocalStreamTimeoutMaxDuration, relaycommon.LocalStreamTimeoutReason(context))
}

func TestScanResponseStartsProgressDeadlineAfterFirstSemanticSignal(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	oldMaxDuration := constant.StreamingLongContextMaxDuration
	oldProgress := constant.StreamingAdaptiveProgressTimeout
	oldInitial := constant.StreamingAdaptiveInitialTimeout
	constant.StreamingTimeout = 5
	constant.StreamingLongContextMaxDuration = 10
	constant.StreamingAdaptiveProgressTimeout = 1
	constant.StreamingAdaptiveInitialTimeout = 0
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
		constant.StreamingLongContextMaxDuration = oldMaxDuration
		constant.StreamingAdaptiveProgressTimeout = oldProgress
		constant.StreamingAdaptiveInitialTimeout = oldInitial
	})

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	relaycommon.MarkLongContextRequest(context, "gpt-5.6-sol", relaycommon.LongContextPromptTokenThreshold)
	relaycommon.MarkSingleChannelRoute(context, true)
	reader, writer := io.Pipe()
	defer writer.Close()
	go func() {
		_, _ = io.WriteString(writer, "data: semantic\n\n")
	}()
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol"}

	ScanResponse(context, &http.Response{Body: reader}, info, func(_ string, result *Result) {
		result.MarkProgress()
	})

	require.Equal(t, gatewaycontract.StreamEndReasonTimeout, info.StreamStatus.EndReason)
	require.Equal(t, relaycommon.LocalStreamTimeoutAdaptiveProgress, relaycommon.LocalStreamTimeoutReason(context))
}

func TestScanResponseRenewsAdaptiveDeadlineOnSemanticProgress(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	oldMaxDuration := constant.StreamingLongContextMaxDuration
	oldProgress := constant.StreamingAdaptiveProgressTimeout
	oldInitial := constant.StreamingAdaptiveInitialTimeout
	constant.StreamingTimeout = 5
	constant.StreamingLongContextMaxDuration = 2
	constant.StreamingAdaptiveProgressTimeout = 1
	constant.StreamingAdaptiveInitialTimeout = 1
	oldPingEnabled := platformgeneral.GetSetting().PingIntervalEnabled
	platformgeneral.GetSetting().PingIntervalEnabled = false
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
		constant.StreamingLongContextMaxDuration = oldMaxDuration
		constant.StreamingAdaptiveProgressTimeout = oldProgress
		constant.StreamingAdaptiveInitialTimeout = oldInitial
		platformgeneral.GetSetting().PingIntervalEnabled = oldPingEnabled
	})

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request, _ = http.NewRequest(http.MethodPost, "http://example.test/v1/responses", nil)
	relaycommon.MarkLongContextRequest(context, "gpt-5.6-sol", relaycommon.LongContextPromptTokenThreshold)
	reader, writer := io.Pipe()
	defer writer.Close()
	go func() {
		_, _ = io.WriteString(writer, "data: first\n\n")
		time.Sleep(700 * time.Millisecond)
		_, _ = io.WriteString(writer, "data: second\n\n")
		time.Sleep(700 * time.Millisecond)
		_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	}()
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol"}

	ScanResponse(context, &http.Response{Body: reader}, info, func(_ string, result *Result) {
		result.MarkProgress()
	})

	require.Equal(t, gatewaycontract.StreamEndReasonDone, info.StreamStatus.EndReason)
}

func TestScanResponseStopsBeforeIdleTimeoutWhenFirstByteIsMissing(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	oldFirstByteTimeout := constant.StreamingFirstByteTimeout
	oldMaxDuration := constant.StreamingMaxDuration
	constant.StreamingTimeout = 5
	constant.StreamingFirstByteTimeout = 1
	constant.StreamingMaxDuration = 10
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
		constant.StreamingFirstByteTimeout = oldFirstByteTimeout
		constant.StreamingMaxDuration = oldMaxDuration
	})

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request, _ = http.NewRequest(http.MethodPost, "http://example.test/v1/responses", nil)
	body := &blockingReader{closed: make(chan struct{})}
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol"}

	started := time.Now()
	ScanResponse(context, &http.Response{Body: body}, info, func(string, *Result) {})

	require.Less(t, time.Since(started), 3*time.Second)
	require.Equal(t, gatewaycontract.StreamEndReasonTimeout, info.StreamStatus.EndReason)
	require.False(t, relaycommon.IsLocalStreamMaxDurationExceeded(context))
}

func TestScanResponseDrainsReadFramesBeforeReturning(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 5
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "http://example.test/v1/responses", nil)
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol"}
	var received []string
	var receivedAt []time.Time
	body := "data: first\n\ndata: second\n\ndata: [DONE]\n\n"

	ScanResponse(context, &http.Response{Body: io.NopCloser(strings.NewReader(body))}, info, func(data string, result *Result) {
		received = append(received, data)
		receivedAt = append(receivedAt, result.ReceivedAt())
		time.Sleep(25 * time.Millisecond)
	})

	require.Equal(t, []string{"first", "second"}, received)
	require.Len(t, receivedAt, 2)
	require.False(t, receivedAt[0].IsZero())
	require.False(t, receivedAt[1].IsZero())
	require.Equal(t, gatewaycontract.StreamEndReasonDone, info.StreamStatus.EndReason)
}

func TestScanResponseCancelsBlockedDataWorkerAfterIdleTimeout(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	oldFirstByteTimeout := constant.StreamingFirstByteTimeout
	oldMaxDuration := constant.StreamingMaxDuration
	constant.StreamingTimeout = 1
	constant.StreamingFirstByteTimeout = 0
	constant.StreamingMaxDuration = 10
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
		constant.StreamingFirstByteTimeout = oldFirstByteTimeout
		constant.StreamingMaxDuration = oldMaxDuration
	})

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "http://example.test/v1/responses", nil)
	reader, writer := io.Pipe()
	defer writer.Close()
	go func() {
		_, _ = io.WriteString(writer, "data: first\n\n")
	}()
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol"}

	started := time.Now()
	ScanResponse(context, &http.Response{Body: reader}, info, func(string, *Result) {
		<-StreamWorkerContext(context).Done()
	})

	require.Less(t, time.Since(started), 3*time.Second)
	require.Equal(t, gatewaycontract.StreamEndReasonTimeout, info.StreamStatus.EndReason)
}

func TestScanResponseInterruptsBlockedScannerWhenHandlerStops(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 5
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "http://example.test/v1/responses", nil)
	reader, writer := io.Pipe()
	defer writer.Close()
	go func() {
		_, _ = io.WriteString(writer, "data: first\n\n")
	}()
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-5.6-sol"}

	started := time.Now()
	ScanResponse(context, &http.Response{Body: reader}, info, func(_ string, result *Result) {
		result.Stop(errors.New("downstream write failed"))
	})

	require.Less(t, time.Since(started), time.Second)
	require.Equal(t, gatewaycontract.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
}
