package app

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestIsModelUnavailableError(t *testing.T) {
	require.True(t, IsModelUnavailableError(types.NewOpenAIError(
		errors.New("The model does not exist"), types.ErrorCodeModelNotFound, http.StatusNotFound,
	)))
	require.True(t, IsModelUnavailableError(types.NewOpenAIError(
		errors.New("model not supported"), types.ErrorCodeBadResponseStatusCode, http.StatusBadRequest,
	)))
	require.False(t, IsModelUnavailableError(types.NewOpenAIError(
		errors.New("invalid API key"), types.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized,
	)))
	require.False(t, IsModelUnavailableError(types.NewOpenAIError(
		errors.New("resource not found"), types.ErrorCodeBadResponseStatusCode, http.StatusNotFound,
	)))
}

func TestCapacityResponseIsRetryableInsteadOfModelScoped(t *testing.T) {
	err := types.NewOpenAIError(
		errors.New("selected model is at capacity. Please try a different model."),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusServiceUnavailable,
	)

	require.Equal(t, upstreamFailureTransient, classifyUpstreamFailure(err))
	require.False(t, IsModelScopedUpstreamFailure(err))
	require.True(t, isRetryableChannelFailure(err))
}

func TestRetryableFailureCooldownExtendsLongContextHeaderTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	relaycommon.MarkLongContextRequest(context, "gpt-5.6-sol", relaycommon.LongContextPromptTokenThreshold)
	timeout := types.NewErrorWithStatusCode(errors.New("timeout awaiting response headers"), types.ErrorCodeChannelResponseTimeExceeded, http.StatusGatewayTimeout)

	require.Equal(t, 45*time.Second, retryableFailureCooldown(context, timeout))
	require.Equal(t, 15*time.Second, retryableFailureCooldown(nil, timeout))
	badGateway := types.NewOpenAIError(errors.New("upstream stream closed"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	require.Equal(t, 8*time.Second, retryableFailureCooldown(nil, badGateway))
}

func TestLongContextFailureDoesNotCoolSharedFaultDomain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	require.True(t, shouldRecordFaultDomainFailure(context))

	relaycommon.MarkLongContextRequest(context, "gpt-5.6-sol", relaycommon.LongContextPromptTokenThreshold)
	require.False(t, shouldRecordFaultDomainFailure(context))
}

func TestIncompleteStreamWithoutContentCoolsLongContextFaultDomain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	relaycommon.MarkLongContextRequest(context, "gpt-5.6-sol", relaycommon.LongContextPromptTokenThreshold)

	require.True(t, shouldRecordIncompleteStreamFaultDomainFailure(context))
	context.Set(string(constant.ContextKeyStreamContentDelivered), true)
	require.False(t, shouldRecordIncompleteStreamFaultDomainFailure(context))
}

func TestIsModelScopedUpstreamFailure(t *testing.T) {
	require.True(t, IsModelScopedUpstreamFailure(types.NewOpenAIError(
		errors.New("insufficient_user_quota"), types.ErrorCodeBadResponseStatusCode, http.StatusServiceUnavailable,
	)))
	require.True(t, IsModelScopedUpstreamFailure(types.NewOpenAIError(
		errors.New("upstream balance exhausted"), types.ErrorCodeBadResponseStatusCode, http.StatusServiceUnavailable,
	)))
	require.True(t, IsModelScopedUpstreamFailure(types.NewOpenAIError(
		errors.New("model unavailable"), types.ErrorCodeModelNotFound, http.StatusNotFound,
	)))
	require.True(t, IsModelScopedUpstreamFailure(types.NewOpenAIError(
		errors.New("upstream timeout"), types.ErrorCodeBadResponseStatusCode, http.StatusServiceUnavailable,
	)))
	require.True(t, IsModelScopedUpstreamFailure(types.NewOpenAIError(
		errors.New("insufficient_user_quota"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden,
	)))
	require.False(t, IsModelScopedUpstreamFailure(types.NewOpenAIError(
		errors.New("access denied"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden,
	)))
}

func TestClassifyUpstreamFailure(t *testing.T) {
	testCases := []struct {
		name     string
		err      *types.NewAPIError
		expected upstreamFailureClass
	}{
		{
			name:     "upstream account exhaustion",
			err:      types.NewOpenAIError(errors.New("upstream balance exhausted"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden),
			expected: upstreamFailureAccountExhausted,
		},
		{
			name:     "response header timeout",
			err:      types.NewErrorWithStatusCode(errors.New("timeout awaiting response headers"), types.ErrorCodeChannelResponseTimeExceeded, http.StatusGatewayTimeout),
			expected: upstreamFailureTransient,
		},
		{
			name:     "closed upstream stream",
			err:      types.NewOpenAIError(errors.New("responses stream closed before response.completed"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError),
			expected: upstreamFailureIncompleteStream,
		},
		{
			name:     "terminated upstream stream",
			err:      types.NewOpenAIError(errors.New("responses stream terminated before response.completed"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
			expected: upstreamFailureIncompleteStream,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, classifyUpstreamFailure(testCase.err))
		})
	}
}
