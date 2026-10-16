package http

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestCreditPackUnsupportedChannelRetriesWithoutUpstreamAttempt(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	gatewayruntime.StartRouteDecision(ctx, "gpt-5", "auto")
	apiErr := types.NewError(fmt.Errorf("funding rejected: %w", billingapp.ErrCreditPackChannelUnsupported), types.ErrorCodeInsufficientUserQuota)
	require.True(t, retryCreditPackUnsupportedChannel(ctx, apiErr, 1))
	decision, found := gatewayruntime.GetRouteDecision(ctx)
	require.True(t, found)
	require.Equal(t, []string{"credit_pack_unsupported"}, decision.Excluded)
	require.Equal(t, 1, decision.RetryCount)
	require.Empty(t, decision.Attempts)
	require.Zero(t, decision.AttemptsUsed)
}

func TestCreditPackUnsupportedChannelStopsWhenRetriesExhausted(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	gatewayruntime.StartRouteDecision(ctx, "gpt-5", "auto")
	apiErr := types.NewError(billingapp.ErrCreditPackChannelUnsupported, types.ErrorCodeInsufficientUserQuota)
	require.False(t, retryCreditPackUnsupportedChannel(ctx, apiErr, 0))
	decision, found := gatewayruntime.GetRouteDecision(ctx)
	require.True(t, found)
	require.Equal(t, []string{"credit_pack_unsupported"}, decision.Excluded)
	require.Zero(t, decision.RetryCount)
	require.Empty(t, decision.Attempts)
}

func TestCreditPackRetryDoesNotRetryOtherBillingFailures(t *testing.T) {
	for _, apiErr := range []*types.NewAPIError{
		nil,
		types.NewError(errors.New("insufficient quota"), types.ErrorCodeInsufficientUserQuota),
		types.NewError(errors.New("channel does not accept credit packs"), types.ErrorCodeUpdateDataError),
	} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		gatewayruntime.StartRouteDecision(ctx, "gpt-5", "auto")
		require.False(t, retryCreditPackUnsupportedChannel(ctx, apiErr, 2))
		decision, found := gatewayruntime.GetRouteDecision(ctx)
		require.True(t, found)
		require.Empty(t, decision.Excluded)
		require.Zero(t, decision.RetryCount)
	}
}
