package stream

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
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
