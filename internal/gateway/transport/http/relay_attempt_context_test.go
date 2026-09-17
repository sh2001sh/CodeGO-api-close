package http

import (
	"context"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestExecuteRelayAttemptCancelsPreviousAttemptBeforeNextAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	parentCtx := context.Background()
	ctx.Request = httptest.NewRequest(stdhttp.MethodPost, "/v1/responses", nil).WithContext(parentCtx)

	var firstAttempt context.Context
	executeRelayAttempt(ctx, func() *types.NewAPIError {
		firstAttempt = ctx.Request.Context()
		require.NoError(t, firstAttempt.Err())
		return nil
	})

	require.ErrorIs(t, firstAttempt.Err(), context.Canceled)
	require.NoError(t, ctx.Request.Context().Err(), "cancelling an upstream attempt must not cancel the downstream request")

	executeRelayAttempt(ctx, func() *types.NewAPIError {
		select {
		case <-firstAttempt.Done():
		default:
			t.Fatal("next channel started before the previous attempt was cancelled")
		}
		require.NoError(t, ctx.Request.Context().Err())
		require.NotEqual(t, firstAttempt, ctx.Request.Context())
		return nil
	})

	require.NoError(t, ctx.Request.Context().Err())
}

func TestExecuteRelayAttemptPropagatesDownstreamCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	parentCtx, cancelParent := context.WithCancel(context.Background())
	ctx.Request = httptest.NewRequest(stdhttp.MethodPost, "/v1/responses", nil).WithContext(parentCtx)

	var attemptCtx context.Context
	executeRelayAttempt(ctx, func() *types.NewAPIError {
		attemptCtx = ctx.Request.Context()
		cancelParent()
		<-attemptCtx.Done()
		return nil
	})

	require.ErrorIs(t, attemptCtx.Err(), context.Canceled)
	require.ErrorIs(t, ctx.Request.Context().Err(), context.Canceled)
}

func TestExecuteRelayAttemptClosesActiveUpstreamTransport(t *testing.T) {
	upstreamStopped := make(chan struct{})
	server := httptest.NewServer(stdhttp.HandlerFunc(func(writer stdhttp.ResponseWriter, request *stdhttp.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: connected\n\n")
		writer.(stdhttp.Flusher).Flush()
		<-request.Context().Done()
		close(upstreamStopped)
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(stdhttp.MethodPost, "/v1/responses", nil)

	executeRelayAttempt(ctx, func() *types.NewAPIError {
		request, err := stdhttp.NewRequestWithContext(ctx.Request.Context(), stdhttp.MethodPost, server.URL, nil)
		require.NoError(t, err)
		response, err := stdhttp.DefaultClient.Do(request)
		require.NoError(t, err)
		require.NotNil(t, response.Body)
		// Simulate a relay returning a retryable failure before its response body
		// has finished. The attempt boundary must cancel this transport.
		return nil
	})

	select {
	case <-upstreamStopped:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream transport remained active after the relay attempt ended")
	}
}
