package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestStreamMaxDurationUsesGPTTiers(t *testing.T) {
	oldNormal := constant.StreamingMaxDuration
	oldLong := constant.StreamingLongContextMaxDuration
	t.Cleanup(func() {
		constant.StreamingMaxDuration = oldNormal
		constant.StreamingLongContextMaxDuration = oldLong
	})
	constant.StreamingMaxDuration = 240
	constant.StreamingLongContextMaxDuration = 540

	require.Equal(t, 240*time.Second, StreamMaxDuration("gpt-5.6-sol", 10_000))
	require.Equal(t, 540*time.Second, StreamMaxDuration("gpt-5.6-sol", LongContextPromptTokenThreshold))
	require.Zero(t, StreamMaxDuration("claude-opus", 10_000))
}

func TestAdaptiveProgressTimeoutUsesLongContextOnly(t *testing.T) {
	oldProgress := constant.StreamingAdaptiveProgressTimeout
	oldInitial := constant.StreamingAdaptiveInitialTimeout
	constant.StreamingAdaptiveProgressTimeout = 45
	constant.StreamingAdaptiveInitialTimeout = 120
	t.Cleanup(func() {
		constant.StreamingAdaptiveProgressTimeout = oldProgress
		constant.StreamingAdaptiveInitialTimeout = oldInitial
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	MarkLongContextRequest(ctx, "gpt-5.6-sol", LongContextPromptTokenThreshold)

	require.Equal(t, 45*time.Second, StreamAdaptiveProgressTimeoutForRequest(ctx, "gpt-5.6-sol", 1))
	require.Equal(t, 120*time.Second, StreamAdaptiveInitialTimeoutForRequest(ctx, "gpt-5.6-sol", 1))
	require.Zero(t, StreamAdaptiveProgressTimeoutForRequest(ctx, "claude-opus", LongContextPromptTokenThreshold))
}

func TestRetryableResponsesFirstAttemptUsesShorterInitialWindow(t *testing.T) {
	oldProgress := constant.StreamingAdaptiveProgressTimeout
	oldInitial := constant.StreamingAdaptiveInitialTimeout
	constant.StreamingAdaptiveProgressTimeout = 45
	constant.StreamingAdaptiveInitialTimeout = 120
	t.Cleanup(func() {
		constant.StreamingAdaptiveProgressTimeout = oldProgress
		constant.StreamingAdaptiveInitialTimeout = oldInitial
	})

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	profile := RequestProfile{
		RequestType: RequestTypeChatLongStream,
		Protocol:    string(types.RelayFormatOpenAIResponses),
		IsStream:    true,
	}
	setRequestProfile(context, profile)
	budget := StartRequestBudget(context, profile, time.Now())
	require.True(t, budget.TryBeginAttempt(time.Now(), "provider:a"))
	MarkLongContextRequest(context, "gpt-5.6-sol", LongContextPromptTokenThreshold)

	require.Equal(t, 60*time.Second, StreamAdaptiveInitialTimeoutForRequest(context, "gpt-5.6-sol", LongContextPromptTokenThreshold))
	require.True(t, budget.TryBeginAttempt(time.Now(), "provider:a"))
	require.Equal(t, 120*time.Second, StreamAdaptiveInitialTimeoutForRequest(context, "gpt-5.6-sol", LongContextPromptTokenThreshold))
}

func TestRetryableShortResponsesUsesSemanticOutputWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	profile := RequestProfile{
		RequestType: RequestTypeChatShortStream,
		Protocol:    string(types.RelayFormatOpenAIResponses),
		IsStream:    true,
	}
	setRequestProfile(context, profile)
	budget := StartRequestBudget(context, profile, time.Now())
	require.True(t, budget.TryBeginAttempt(time.Now(), "provider:a"))

	require.Zero(t, StreamAdaptiveProgressTimeoutForRequest(context, "gpt-5.6-sol", 1_000))
	require.Equal(t, 60*time.Second, StreamAdaptiveInitialTimeoutForRequest(context, "gpt-5.6-sol", 1_000))
}

func TestSingleChannelResponsesHasNoAdaptiveInitialDeadline(t *testing.T) {
	oldProgress := constant.StreamingAdaptiveProgressTimeout
	oldInitial := constant.StreamingAdaptiveInitialTimeout
	constant.StreamingAdaptiveProgressTimeout = 45
	constant.StreamingAdaptiveInitialTimeout = 120
	t.Cleanup(func() {
		constant.StreamingAdaptiveProgressTimeout = oldProgress
		constant.StreamingAdaptiveInitialTimeout = oldInitial
	})

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	MarkLongContextRequest(context, "gpt-5.6-sol", LongContextPromptTokenThreshold)
	MarkSingleChannelRoute(context, true)

	require.Zero(t, StreamAdaptiveInitialTimeoutForRequest(context, "gpt-5.6-sol", LongContextPromptTokenThreshold))
	require.Equal(t, 45*time.Second, StreamAdaptiveProgressTimeoutForRequest(context, "gpt-5.6-sol", LongContextPromptTokenThreshold))
}
