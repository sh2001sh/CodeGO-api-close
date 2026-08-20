package http

import (
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestShouldRecordAutoGroupFailureIndependentOfReplaySafety(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	gatewayruntime.MarkRemainingCrossGroupRoutes(ctx, 1)
	retryable := types.NewOpenAIError(
		errors.New("upstream temporarily unavailable"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusServiceUnavailable,
	)

	require.True(t, shouldRecordAutoGroupFailure(ctx, retryable))

	ctx.Set(string(constant.ContextKeyStreamContentDelivered), true)
	require.True(t, shouldRecordAutoGroupFailure(ctx, retryable), "committed output blocks replay but must still cool the failed route")

	ctx.Set(string(constant.ContextKeyClientGone), true)
	require.False(t, shouldRecordAutoGroupFailure(ctx, retryable))
}

func TestShouldRecordAutoGroupFailureSkipsLocalAndDeterministicErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	gatewayruntime.MarkRemainingCrossGroupRoutes(ctx, 1)
	deterministic := types.NewOpenAIError(
		errors.New("invalid request"),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
	)
	require.False(t, shouldRecordAutoGroupFailure(ctx, deterministic))

	gatewayruntime.MarkLocalStreamMaxDurationExceeded(ctx)
	retryable := types.NewOpenAIError(
		errors.New("gateway timeout"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusGatewayTimeout,
	)
	require.False(t, shouldRecordAutoGroupFailure(ctx, retryable))
}
