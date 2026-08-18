package http

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystream "github.com/sh2001sh/new-api/internal/gateway/stream"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type relayErrorEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func TestRefundRelayBillingSkipsRequestsWithoutRelayInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	apiErr := types.NewErrorWithStatusCode(errors.New("service busy"), types.ErrorCodeServiceBusy, http.StatusServiceUnavailable)

	require.Same(t, apiErr, refundRelayBillingIfNeeded(ctx, nil, apiErr))
}

func TestRelayFailureSampleRequiresUpstreamAttempt(t *testing.T) {
	apiErr := types.NewErrorWithStatusCode(
		errors.New("用户额度不足"),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
	)

	require.False(t, shouldRecordRelayFailureSample(false, apiErr))
	require.True(t, shouldRecordRelayFailureSample(true, apiErr))
	require.False(t, shouldRecordRelayFailureSample(true, nil))
}

func TestFinalRelayFailureLogRespectsNoRecordFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 1)

	recordable := types.NewErrorWithStatusCode(
		errors.New("upstream unavailable"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusServiceUnavailable,
	)
	localQuota := types.NewErrorWithStatusCode(
		errors.New("用户额度不足"),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
		types.ErrOptionWithNoRecordErrorLog(),
	)

	require.True(t, shouldRecordFinalRelayFailureLog(ctx, recordable))
	require.False(t, shouldRecordFinalRelayFailureLog(ctx, localQuota))
	require.False(t, shouldRecordFinalRelayFailureLog(nil, recordable))
}

func TestRetryChannelReusableHonorsOnlyGlobalChannelStatus(t *testing.T) {
	enabled := &gatewayschema.Channel{Id: 6, Status: constant.ChannelStatusEnabled}
	require.True(t, isRetryChannelReusable(enabled))

	disabled := &gatewayschema.Channel{Id: 6, Status: constant.ChannelStatusAutoDisabled}
	require.False(t, isRetryChannelReusable(disabled))
	require.False(t, isRetryChannelReusable(nil))
}

func TestShouldRetryGatewayTimeoutBeforeResponseDelivery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	err := types.NewOpenAIError(errors.New("upstream timeout"), types.ErrorCodeBadResponseStatusCode, 524)

	require.True(t, shouldRetry(ctx, err, 1))
	httpctx.SetContextKey(ctx, constant.ContextKeyResponseBodyDelivered, true)
	require.False(t, shouldRetry(ctx, err, 1))
}

func TestShouldNotRetryAfterResponsesCreateWasWrittenUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set(string(constant.ContextKeyResponsesReplayForbidden), true)
	err := types.NewOpenAIError(errors.New("upstream websocket closed"), types.ErrorCodeBadResponseStatusCode, 524)

	require.False(t, shouldRetry(ctx, err, 1))
}

func TestShouldRetryCapacityBeforeResponseDelivery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("original_model", "gpt-5.6-sol")
	err := types.NewOpenAIError(
		errors.New("selected model is at capacity. Please try a different model."),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusServiceUnavailable,
	)

	require.True(t, shouldRetry(ctx, err, 1))
	httpctx.SetContextKey(ctx, constant.ContextKeyResponseBodyDelivered, true)
	require.False(t, shouldRetry(ctx, err, 1))
}

func TestShouldRetryGPTFailureOnlyWithinInitialWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	err := types.NewOpenAIError(errors.New("upstream timeout"), types.ErrorCodeBadResponseStatusCode, http.StatusGatewayTimeout)

	t.Run("retries an early GPT failure", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		ctx.Set("original_model", "gpt-5.6-sol")
		httpctx.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now().Add(-relaycommon.GPTInitialFailureRetryWindow+time.Second))

		require.True(t, shouldRetry(ctx, err, 1))
	})

	t.Run("does not restart a late GPT failure", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		ctx.Set("original_model", "gpt-5.6-sol")
		httpctx.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now().Add(-relaycommon.GPTInitialFailureRetryWindow-time.Second))

		require.False(t, shouldRetry(ctx, err, 1))
	})

	t.Run("does not change non GPT retry behavior", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		ctx.Set("original_model", "claude-opus-5")
		httpctx.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now().Add(-relaycommon.GPTInitialFailureRetryWindow-time.Second))

		require.True(t, shouldRetry(ctx, err, 1))
	})
}

func TestShouldRetryResponsesStreamBeforeContentDespiteLifecycleEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	err := types.NewOpenAIError(errors.New("responses stream closed before response.completed"), types.ErrorCodeBadResponse, http.StatusBadGateway)

	httpctx.SetContextKey(ctx, constant.ContextKeyResponseBodyDelivered, true)
	ctx.Set(string(constant.ContextKeyResponsesStreamRetrySafe), true)
	require.True(t, shouldRetry(ctx, err, 1))

	gatewaystream.MarkSemanticCommitted(ctx)
	require.False(t, shouldRetry(ctx, err, 1))
}

func TestFinalizeRelayErrorWritesSanitizedResponsesStreamErrorAfterCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	httpctx.SetContextKey(ctx, constant.ContextKeyIsStream, true)
	httpctx.SetContextKey(ctx, constant.ContextKeyResponseBodyDelivered, true)
	apiErr := types.NewOpenAIError(
		errors.New("upstream channel #67 responses stream closed before response.completed (request id: hidden)"),
		types.ErrorCodeBadResponse,
		http.StatusBadGateway,
	)

	finalizeRelayError(ctx, types.RelayFormatOpenAI, nil, apiErr, "downstream")

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "event: error")
	require.Contains(t, recorder.Body.String(), types.ModelUnavailableMessage)
	require.Contains(t, recorder.Body.String(), "downstream")
	require.NotContains(t, recorder.Body.String(), "channel #67")
	require.NotContains(t, recorder.Body.String(), "hidden")
}

func TestFinalizeRelayErrorSkipsCancelledClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set(string(constant.ContextKeyClientGone), true)
	apiErr := types.NewOpenAIError(
		errors.New("responses stream closed before response.completed"),
		types.ErrorCodeBadResponse,
		http.StatusBadGateway,
	)

	finalizeRelayError(ctx, types.RelayFormatOpenAI, nil, apiErr, "downstream")

	require.Empty(t, recorder.Body.String())
}

func TestFinalizeRelayErrorDoesNotAppendAfterResponsesTerminalEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	httpctx.SetContextKey(ctx, constant.ContextKeyIsStream, true)
	httpctx.SetContextKey(ctx, constant.ContextKeyResponseBodyDelivered, true)
	ctx.Set(string(constant.ContextKeyResponsesTerminalSent), true)
	apiErr := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponse, http.StatusBadGateway)

	finalizeRelayError(ctx, types.RelayFormatOpenAIResponses, nil, apiErr, "downstream")
	require.Empty(t, recorder.Body.String())
}

func TestShouldNotRetryAfterClientDisconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set(string(constant.ContextKeyClientGone), true)
	err := types.NewOpenAIError(errors.New("responses stream closed before response.completed"), types.ErrorCodeBadResponse, http.StatusBadGateway)

	require.False(t, shouldRetry(ctx, err, 1))
}

func TestShouldRetryLateResponsesStreamBeforeSemanticContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("original_model", "gpt-5.6-sol")
	httpctx.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now().Add(-2*relaycommon.GPTInitialFailureRetryWindow))
	httpctx.SetContextKey(ctx, constant.ContextKeyResponseBodyDelivered, true)
	ctx.Set(string(constant.ContextKeyResponsesStreamRetrySafe), true)
	err := types.NewOpenAIError(errors.New("upstream produced no semantic output"), types.ErrorCodeChannelResponseTimeExceeded, http.StatusGatewayTimeout)

	require.True(t, shouldRetry(ctx, err, 1))
}

func TestShouldRetryResponsesFailureAfterLegacyNinetySecondBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("original_model", "gpt-5.6-sol")
	ctx.Set(string(constant.ContextKeyResponsesStreamRetrySafe), true)
	startedAt := time.Now().Add(-121 * time.Second)
	httpctx.SetContextKey(ctx, constant.ContextKeyRequestStartTime, startedAt)
	profile := relaycommon.InitializeRequestProfile(
		ctx,
		"gpt-5.6-sol",
		ctx.Request.URL.Path,
		relaycommon.RequestProfileHint{IsStream: true, HasUpstreamState: true},
	)
	budget := relaycommon.StartRequestBudget(ctx, profile, startedAt)
	require.True(t, budget.TryBeginAttempt(startedAt, "provider:a"))
	err := types.NewOpenAIError(
		errors.New("responses stream closed before response.completed"),
		types.ErrorCodeBadResponse,
		http.StatusBadGateway,
	)

	require.True(t, shouldRetry(ctx, err, 1))
}

func TestRouteSelectionWaitIsLimitedAndHonorsCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestContext, cancel := context.WithCancel(context.Background())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestContext)
	ctx.Set(routeSelectionExhaustedContextKey, true)

	require.True(t, shouldWaitForRouteSelection(ctx, 0))
	require.True(t, shouldWaitForRouteSelection(ctx, 1))
	require.False(t, shouldWaitForRouteSelection(ctx, 2))

	cancel()
	started := time.Now()
	require.False(t, waitForRouteSelection(ctx, 0))
	require.Less(t, time.Since(started), 100*time.Millisecond)
}

func TestFinalizeRelayErrorMasksChineseUpstreamQuotaLeak(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	apiErr := types.NewOpenAIError(
		errors.New("用户额度不足, 剩余额度: -＄0.038392 (request id: upstream)"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	finalizeRelayError(ctx, types.RelayFormatOpenAI, nil, apiErr, "downstream")

	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, platformtext.UpstreamQuotaGenericMessage+" (request id: downstream)", response.Error.Message)
}

func TestFinalizeRelayErrorKeepsLocalQuotaMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	apiErr := types.NewErrorWithStatusCode(
		errors.New("用户额度不足, 剩余额度: ＄0.002290"),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
	)

	finalizeRelayError(ctx, types.RelayFormatOpenAI, nil, apiErr, "downstream")

	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Contains(t, response.Error.Message, "用户额度不足, 剩余额度: ＄0.002290")
	require.NotEqual(t, platformtext.UpstreamQuotaGenericMessage, response.Error.Message)
}

func TestFinalizeRelayErrorHidesLocalChannelSelectionDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	apiErr := types.NewError(
		errors.New("分组 plus高不稳定分组 下模型 gpt-5.6-luna 的可用渠道不存在（retry）"),
		types.ErrorCodeGetChannelFailed,
	)

	finalizeRelayError(ctx, types.RelayFormatOpenAI, nil, apiErr, "downstream")

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Contains(t, response.Error.Message, types.ModelUnavailableMessage)
	require.NotContains(t, response.Error.Message, "plus高不稳定分组")
}

func TestFinalizeRelayErrorHidesUpstreamChannelAvailabilityDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	apiErr := types.NewOpenAIError(
		errors.New("No available channel for model gpt-5.6-luna under group plus高不稳定分组 (distributor) (request id: upstream)"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadGateway,
	)

	finalizeRelayError(ctx, types.RelayFormatOpenAI, nil, apiErr, "downstream")

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, types.ModelUnavailableMessage+" (request id: downstream)", response.Error.Message)
	require.Equal(t, string(types.ErrorCodeGetChannelFailed), response.Error.Code)
	require.NotContains(t, recorder.Body.String(), "plus高不稳定分组")
	require.NotContains(t, recorder.Body.String(), "upstream")
}

func TestFinalizeRelayErrorHidesAnyUpstreamServiceUnavailableMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	apiErr := types.NewOpenAIError(
		errors.New("provider capacity temporarily exhausted (trace: upstream)"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusServiceUnavailable,
	)

	finalizeRelayError(ctx, types.RelayFormatOpenAI, nil, apiErr, "downstream")

	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, types.ModelUnavailableMessage+" (request id: downstream)", response.Error.Message)
	require.NotContains(t, recorder.Body.String(), "upstream")
}

func TestRelayNotImplementedReturnsOpenAIStyleError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/files", nil)

	RelayNotImplemented(ctx)

	require.Equal(t, http.StatusNotImplemented, recorder.Code)
	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "API not implemented", response.Error.Message)
	require.Equal(t, "new_api_error", response.Error.Type)
	require.Equal(t, "api_not_implemented", response.Error.Code)
}

func TestRelayNotFoundIncludesMethodAndPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/unknown", nil)

	RelayNotFound(ctx)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "Invalid URL (GET /v1/unknown)", response.Error.Message)
	require.Equal(t, "invalid_request_error", response.Error.Type)
}

func TestPlaygroundRejectsAccessToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/chat/completions", nil)
	ctx.Set("use_access_token", true)

	Playground(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "暂不支持使用 access token", response.Error.Message)
}

func TestPlaygroundImageRejectsAccessToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/images/generations", nil)
	ctx.Set("use_access_token", true)

	PlaygroundImage(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	var response relayErrorEnvelope
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "暂不支持使用 access token", response.Error.Message)
}
