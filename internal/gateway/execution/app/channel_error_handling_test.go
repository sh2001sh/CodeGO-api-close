package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystream "github.com/sh2001sh/new-api/internal/gateway/stream"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestCancelledHeaderWaitDoesNotRecordChannelFailure(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestCtx, cancel := context.WithCancel(context.Background())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestCtx)
	ctx.Set("original_model", "gpt-cancelled-header-test")
	ctx.Set(constant.RequestIdKey, t.Name())
	httpctx.SetContextKey(ctx, constant.ContextKeyUserId, 924991)
	relaycommon.MarkAutoRouteRequest(ctx)
	relaycommon.MarkRemainingCrossGroupRoutes(ctx, 3)
	cancel()
	err := types.NewOpenAIError(context.Canceled, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	ProcessChannelError(ctx, *types.NewChannelError(924991, constant.ChannelTypeOpenAI, "cancelled", false, "", false), err)
	require.True(t, ctx.GetBool(string(constant.ContextKeyClientGone)))
	_, shared := relaycommon.GetChannelHealth(924991, "gpt-cancelled-header-test")
	_, user := relaycommon.GetUserChannelHealth(ctx, 924991, "gpt-cancelled-header-test")
	require.False(t, shared)
	require.False(t, user)
}

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

func TestContentPolicyRefusalDoesNotPoisonRouteHealth(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("original_model", "gpt-5.6-sol")
	ctx.Set(constant.RequestIdKey, t.Name())
	httpctx.SetContextKey(ctx, constant.ContextKeyUserId, 923481)
	relaycommon.MarkAutoRouteRequest(ctx)
	relaycommon.MarkRemainingCrossGroupRoutes(ctx, 2)
	err := types.NewOpenAIError(errors.New("request blocked by our safety systems"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	require.False(t, isRetryableChannelFailure(err))
	require.Equal(t, upstreamFailureContentPolicy, classifyUpstreamFailure(err))
	ProcessChannelError(ctx, *types.NewChannelError(923481, constant.ChannelTypeOpenAI, "policy", true, "", false), err)
	_, shared := relaycommon.GetChannelHealth(923481, "gpt-5.6-sol")
	_, user := relaycommon.GetUserChannelHealth(ctx, 923481, "gpt-5.6-sol")
	require.False(t, shared)
	require.False(t, user)
	require.Zero(t, httpctx.GetContextKeyInt(ctx, constant.ContextKeyRetryFallbackChannelID))
}

func TestExplicitUpstreamCredentialRejectionIsRetryable(t *testing.T) {
	err := types.NewOpenAIError(
		errors.New("Upstream access forbidden, please contact administrator"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	require.Equal(t, upstreamFailureCredentialRejected, classifyUpstreamFailure(err))
	require.True(t, IsUpstreamCredentialRejectedError(err))
	require.True(t, isRetryableChannelFailure(err))
}

func TestDisabledUpstreamAPIKeyGroupCoolsChannel(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set(constant.RequestIdKey, t.Name())
	ctx.Set("original_model", "gpt-disabled-group-test")
	relaycommon.MarkAutoRouteRequest(ctx)
	relaycommon.MarkRemainingCrossGroupRoutes(ctx, 2)
	err := types.NewOpenAIError(errors.New("API Key 所属分组已停用"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)
	require.Equal(t, upstreamFailureCredentialRejected, classifyUpstreamFailure(err))
	require.True(t, isRetryableChannelFailure(err))
	ProcessChannelError(ctx, *types.NewChannelError(924992, constant.ChannelTypeOpenAI, "disabled-group", false, "", false), err)
	require.True(t, relaycommon.IsChannelCredentialCooling(924992))
	for _, message := range []string{"access denied", "request blocked by our safety systems"} {
		require.False(t, IsUpstreamCredentialRejectedError(types.NewOpenAIError(errors.New(message), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)))
	}
}

func TestDatabaseConnectionExhaustionIsRetryableTransientFailure(t *testing.T) {
	err := types.NewOpenAIError(
		errors.New("failed to connect to database: remaining connection slots are reserved (SQLSTATE 53300)"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
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

func TestRetryCurrentChannelOnlyBeforeDownstreamOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	fastTransportFailure := types.NewErrorWithStatusCode(
		errors.New("connection reset by peer"),
		types.ErrorCodeDoRequestFailed,
		http.StatusBadGateway,
	)
	context.Set("use_channel", []string{"72"})
	context.Set(string(constant.ContextKeyRequestStartTime), time.Now())

	require.True(t, shouldRetryCurrentChannelIfNoAlternative(context, fastTransportFailure))
	context.Set("use_channel", []string{"72", "73"})
	require.False(t, shouldRetryCurrentChannelIfNoAlternative(context, fastTransportFailure))
	context.Set("use_channel", []string{"72"})

	context.Set(string(constant.ContextKeyResponseBodyDelivered), true)
	require.False(t, shouldRetryCurrentChannelIfNoAlternative(context, fastTransportFailure))
	context.Set(string(constant.ContextKeyResponseBodyDelivered), false)
	context.Set(string(constant.ContextKeyRequestStartTime), time.Now().Add(-currentChannelRetryMaxElapsed-time.Millisecond))
	require.False(t, shouldRetryCurrentChannelIfNoAlternative(context, fastTransportFailure))

	headerTimeout := types.NewErrorWithStatusCode(
		errors.New("timeout awaiting response headers"),
		types.ErrorCodeChannelResponseTimeExceeded,
		http.StatusGatewayTimeout,
	)
	require.True(t, shouldRetryCurrentChannelIfNoAlternative(context, headerTimeout))

	context.Set(string(constant.ContextKeyRequestStartTime), time.Now().Add(-30*time.Second))
	providerTimeout := types.NewOpenAIError(
		errors.New("provider timed out"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusGatewayTimeout,
	)
	require.True(t, shouldRetryCurrentChannelIfNoAlternative(context, providerTimeout))
}

func TestRetryCurrentChannelForResponsesBeforeSemanticOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	context.Set("use_channel", []string{"72"})
	context.Set(string(constant.ContextKeyResponsesStreamRetrySafe), true)
	context.Set(string(constant.ContextKeyRelayAttemptStage), gatewaystream.AttemptStageBootstrap)
	context.Set(string(constant.ContextKeyRequestStartTime), time.Now().Add(-30*time.Second))

	streamClosed := types.NewOpenAIError(
		errors.New("responses stream closed before response.completed"),
		types.ErrorCodeBadResponse,
		http.StatusBadGateway,
	)
	require.True(t, shouldRetryCurrentChannelIfNoAlternative(context, streamClosed))
	context.Set("use_channel", []string{"72", "72"})
	require.True(t, shouldRetryCurrentChannelIfNoAlternative(context, streamClosed))
	context.Set("use_channel", []string{"72", "73"})
	require.False(t, shouldRetryCurrentChannelIfNoAlternative(context, streamClosed))
	context.Set("use_channel", []string{"72"})

	context.Set(string(constant.ContextKeyStreamContentDelivered), true)
	require.False(t, shouldRetryCurrentChannelIfNoAlternative(context, streamClosed))
}

func TestResponsesOnlyRouteKeepsRetryFallbackBeforeSemanticOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalHasAlternative := hasAlternativeSelectableRoute
	hasAlternativeSelectableRoute = func(int, string, string) (bool, error) { return false, nil }
	t.Cleanup(func() { hasAlternativeSelectableRoute = originalHasAlternative })

	for _, stage := range []gatewaystream.AttemptStage{
		gatewaystream.AttemptStageConnected,
		gatewaystream.AttemptStageBootstrap,
	} {
		t.Run(string(stage), func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			context.Set("original_model", "gpt-6-astra")
			context.Set("use_channel", []string{"61001"})
			context.Set(string(constant.ContextKeyResponsesStreamRetrySafe), true)
			context.Set(string(constant.ContextKeyRelayAttemptStage), stage)
			httpctx.SetContextKey(context, constant.ContextKeyUsingGroup, "pro-only")

			ProcessChannelError(context, *types.NewChannelError(61001, constant.ChannelTypeOpenAI, "sole", false, "", false), types.NewOpenAIError(
				errors.New("upstream connection reset before semantic output"),
				types.ErrorCodeBadResponseStatusCode,
				http.StatusBadGateway,
			))

			require.Equal(t, 61001, httpctx.GetContextKeyInt(context, constant.ContextKeyRetryFallbackChannelID))
		})
	}
}

func TestIncompleteStreamOnOnlyRouteDoesNotCoolModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	context.Set("original_model", "gpt-5.6-sol")
	httpctx.SetContextKey(context, constant.ContextKeyUsingGroup, "plus-only-route")

	channelID := 9087
	modelName := "gpt-5.6-sol"
	originalHasAlternative := hasAlternativeSelectableRoute
	hasAlternativeSelectableRoute = func(id int, group, model string) (bool, error) {
		require.Equal(t, channelID, id)
		require.Equal(t, "plus-only-route", group)
		require.Equal(t, modelName, model)
		return false, nil
	}
	t.Cleanup(func() {
		hasAlternativeSelectableRoute = originalHasAlternative
	})

	ProcessChannelError(context, *types.NewChannelError(channelID, constant.ChannelTypeOpenAI, "sole", false, "", false), types.NewOpenAIError(
		errors.New("responses stream closed before response.completed"),
		types.ErrorCodeBadResponse,
		http.StatusBadGateway,
	))

	_, cooled := relaycommon.GetChannelHealth(channelID, modelName)
	require.False(t, cooled)
}

func TestOnlyRoute429RetriesCurrentChannelWithoutCooling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	context.Set("original_model", "gpt-5.6-sol")
	context.Set("use_channel", []string{"6"})
	context.Set(string(constant.ContextKeyRelayAttemptStage), gatewaystream.AttemptStageSelected)
	context.Set("channel_affinity_skip_retry_on_failure", true)
	httpctx.SetContextKey(context, constant.ContextKeyUsingGroup, "plus-only-route")

	originalHasAlternative := hasAlternativeSelectableRoute
	hasAlternativeSelectableRoute = func(id int, group, model string) (bool, error) {
		require.Equal(t, 6, id)
		require.Equal(t, "plus-only-route", group)
		require.Equal(t, "gpt-5.6-sol", model)
		return false, nil
	}
	t.Cleanup(func() {
		hasAlternativeSelectableRoute = originalHasAlternative
	})

	ProcessChannelError(context, *types.NewChannelError(6, constant.ChannelTypeOpenAI, "sole", false, "", false), types.NewOpenAIError(
		errors.New("bad response status code 429"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusTooManyRequests,
	))

	require.Equal(t, 6, httpctx.GetContextKeyInt(context, constant.ContextKeyRetryFallbackChannelID))
	require.False(t, relaycommon.ShouldSkipRetryAfterChannelAffinityFailure(context))
	_, cooled := relaycommon.GetChannelHealth(6, "gpt-5.6-sol")
	require.False(t, cooled)
}

func TestResponses524InvalidatesCacheAffinityAndAllowsUnusedKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	context.Set("original_model", "gpt-5")
	context.Set("use_channel", []string{"6"})
	context.Set(string(constant.ContextKeyRelayAttemptStage), gatewaystream.AttemptStageSelected)
	context.Set("channel_affinity_skip_retry_on_failure", true)
	httpctx.SetContextKey(context, constant.ContextKeyUsingGroup, "sole")
	relaycommon.InitializeRequestProfile(context, "gpt-5", "/v1/responses", relaycommon.RequestProfileHint{IsStream: true, HasCacheAffinity: true})

	originalHasAlternative := hasAlternativeSelectableRoute
	hasAlternativeSelectableRoute = func(int, string, string) (bool, error) { return false, nil }
	t.Cleanup(func() { hasAlternativeSelectableRoute = originalHasAlternative })

	ProcessChannelError(context, *types.NewChannelError(6, constant.ChannelTypeOpenAI, "sole", true, "", false), types.NewOpenAIError(
		errors.New("524 upstream read timeout"), types.ErrorCodeBadResponseStatusCode, 524,
	))

	require.True(t, context.GetBool(requestAllowMultiKeyMigrationContextKey))
	require.False(t, relaycommon.ShouldSkipRetryAfterChannelAffinityFailure(context))
	require.Equal(t, 6, httpctx.GetContextKeyInt(context, constant.ContextKeyRetryFallbackChannelID))
}

func TestResponsesStateBoundTimeoutKeepsOriginalRouteAndKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	context.Set("original_model", "gpt-5")
	context.Set("use_channel", []string{"6"})
	context.Set(string(constant.ContextKeyRelayAttemptStage), gatewaystream.AttemptStageSelected)
	httpctx.SetContextKey(context, constant.ContextKeyUsingGroup, "primary")
	relaycommon.InitializeRequestProfile(context, "gpt-5", "/v1/responses", relaycommon.RequestProfileHint{IsStream: true, HasUpstreamState: true})
	relaycommon.MarkRemainingCrossGroupRoutes(context, 1)

	ProcessChannelError(context, *types.NewChannelError(6, constant.ChannelTypeOpenAI, "primary", true, "", false), types.NewOpenAIError(
		errors.New("524 upstream read timeout"), types.ErrorCodeBadResponseStatusCode, 524,
	))

	require.False(t, context.GetBool(requestAllowMultiKeyMigrationContextKey))
	require.Equal(t, 6, httpctx.GetContextKeyInt(context, constant.ContextKeyRetryFallbackChannelID))
}

func TestModelUnavailableRetriesNextAutoGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	context.Set("original_model", "gpt-5.6-sol")
	context.Set("use_channel", []string{"33"})
	context.Set("channel_affinity_skip_retry_on_failure", true)
	relaycommon.MarkRemainingCrossGroupRoutes(context, 2)
	httpctx.SetContextKey(context, constant.ContextKeyUsingGroup, "market-primary")

	originalHasAlternative := hasAlternativeSelectableRoute
	hasAlternativeSelectableRoute = func(int, string, string) (bool, error) {
		return false, nil
	}
	t.Cleanup(func() { hasAlternativeSelectableRoute = originalHasAlternative })

	ProcessChannelError(context, *types.NewChannelError(33, constant.ChannelTypeOpenAI, "primary", false, "", false), types.NewOpenAIError(
		errors.New("model temporarily unavailable"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusServiceUnavailable,
	))

	require.True(t, context.GetBool("model_unavailable_with_alternative"))
	require.False(t, relaycommon.ShouldSkipRetryAfterChannelAffinityFailure(context))
}

func TestSlowFirstOutputMovesToNextAutoGroupWithoutRetryingCurrentChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	context.Set("original_model", "gpt-5.6-sol")
	context.Set("use_channel", []string{"33001"})
	context.Set("channel_affinity_skip_retry_on_failure", true)
	context.Set(string(constant.ContextKeyResponsesStreamRetrySafe), true)
	context.Set(string(constant.ContextKeyRelayAttemptStage), gatewaystream.AttemptStageBootstrap)
	relaycommon.MarkRemainingCrossGroupRoutes(context, 1)
	httpctx.SetContextKey(context, constant.ContextKeyUsingGroup, "market-primary")

	originalHasAlternative := hasAlternativeSelectableRoute
	hasAlternativeSelectableRoute = func(int, string, string) (bool, error) {
		return false, nil
	}
	t.Cleanup(func() {
		hasAlternativeSelectableRoute = originalHasAlternative
		relaycommon.RecordChannelSuccess(33001, "gpt-5.6-sol", 0)
	})

	ProcessChannelError(context, *types.NewChannelError(33001, constant.ChannelTypeOpenAI, "primary", false, "", false), types.NewOpenAIError(
		errors.New("upstream produced no semantic output before 60s"),
		types.ErrorCodeChannelResponseTimeExceeded,
		http.StatusGatewayTimeout,
	))

	require.Zero(t, httpctx.GetContextKeyInt(context, constant.ContextKeyRetryFallbackChannelID))
	require.False(t, relaycommon.ShouldSkipRetryAfterChannelAffinityFailure(context))
	_, cooled := relaycommon.GetChannelHealth(33001, "gpt-5.6-sol")
	require.True(t, cooled)
}

func TestLongContextFailureDoesNotCoolSharedFaultDomain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	err := types.NewErrorWithStatusCode(errors.New("upstream timeout"), types.ErrorCodeChannelResponseTimeExceeded, http.StatusGatewayTimeout)
	require.True(t, shouldRecordFaultDomainFailure(context, err))

	relaycommon.MarkLongContextRequest(context, "gpt-5.6-sol", relaycommon.LongContextPromptTokenThreshold)
	require.False(t, shouldRecordFaultDomainFailure(context, err))
}

func TestIncompleteStreamWithoutContentCoolsLongContextFaultDomain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	relaycommon.MarkLongContextRequest(context, "gpt-5.6-sol", relaycommon.LongContextPromptTokenThreshold)
	err := types.NewErrorWithStatusCode(errors.New("upstream stream closed"), types.ErrorCodeChannelResponseTimeExceeded, http.StatusGatewayTimeout)

	require.True(t, shouldRecordIncompleteStreamFaultDomainFailure(context, err))
	context.Set(string(constant.ContextKeyStreamContentDelivered), true)
	require.False(t, shouldRecordIncompleteStreamFaultDomainFailure(context, err))
}

func TestIncompleteStreamExcludesFaultDomainOnlyBeforeSemanticContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)

	require.True(t, shouldExcludeFaultDomainForIncompleteStream(context))
	context.Set(string(constant.ContextKeyStreamContentDelivered), true)
	require.False(t, shouldExcludeFaultDomainForIncompleteStream(context))
}

func TestLocalStreamMaxDurationIsNotTreatedAsUpstreamFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	relaycommon.MarkLocalStreamMaxDurationExceeded(context)

	require.True(t, isLocalStreamMaxDuration(context))
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
