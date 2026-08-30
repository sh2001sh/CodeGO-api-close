package runtime

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRequestBudgetBoundsAttemptsAndFaultDomains(t *testing.T) {
	now := time.Now()
	budget := StartRequestBudget(nil, RequestProfile{RequestType: RequestTypeChatShortStream}, now)

	require.True(t, budget.TryBeginAttempt(now, "provider:a"))
	require.True(t, budget.CanRetry(now))
	require.True(t, budget.TryBeginAttempt(now, "provider:b"))
	require.False(t, budget.CanRetry(now))
	require.False(t, budget.TryBeginAttempt(now, "provider:c"))
	require.Equal(t, 2, budget.AttemptsUsed)
	require.Equal(t, 2, budget.FaultDomainsUsed)
}

func TestRequestBudgetDoesNotResetOrRetryAfterDeadline(t *testing.T) {
	startedAt := time.Now().Add(-time.Minute)
	budget := StartRequestBudget(nil, RequestProfile{RequestType: RequestTypeChatShortStream}, startedAt)

	require.False(t, budget.TryBeginAttempt(time.Now(), "provider:a"))
	require.False(t, budget.CanRetry(time.Now()))
	require.Zero(t, budget.Remaining(time.Now()))
}

func TestStartRequestBudgetReusesContextBudgetAcrossRetries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	startedAt := time.Now()
	profile := RequestProfile{RequestType: RequestTypeChatShortStream}
	budget := StartRequestBudget(context, profile, startedAt)
	require.True(t, budget.TryBeginAttempt(startedAt, "provider:a"))

	reused := StartRequestBudget(context, profile, startedAt.Add(time.Minute))

	require.Same(t, budget, reused)
	require.Equal(t, 1, reused.AttemptsUsed)
	require.Equal(t, startedAt, reused.StartedAt)
}

func TestResponsesStreamBudgetKeepsOneRecoveryAttemptAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	now := time.Now()
	profile := RequestProfile{
		RequestType: RequestTypeToolCallStream,
		Protocol:    string(types.RelayFormatOpenAIResponses),
		IsStream:    true,
	}
	setRequestProfile(context, profile)
	budget := StartRequestBudget(context, profile, now)

	require.Equal(t, responsesStreamRetryBudget, budget.Deadline.Sub(now))
	require.True(t, budget.TryBeginAttempt(now, "provider:a"))
	require.Equal(t, responsesFirstAttemptWaitTimeout, RetryableResponsesAttemptTimeout(context))
	require.True(t, budget.TryBeginAttempt(now.Add(time.Second), "provider:a"))
	require.Zero(t, RetryableResponsesAttemptTimeout(context))
}

func TestResponsesStreamSingleChannelDoesNotUseFastAttemptTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	profile := RequestProfile{
		RequestType: RequestTypeChatLongStream,
		Protocol:    string(types.RelayFormatOpenAIResponses),
		IsStream:    true,
	}
	setRequestProfile(context, profile)
	budget := StartRequestBudget(context, profile, time.Now())
	require.True(t, budget.TryBeginAttempt(time.Now(), "provider:a"))
	MarkSingleChannelRoute(context, true)

	require.Zero(t, RetryableResponsesAttemptTimeout(context))
	require.True(t, budget.TryBeginAttempt(time.Now(), "provider:a"))
	require.Equal(t, 1, budget.FaultDomainsUsed)
}

func TestResponsesStreamSingleChannelUsesTimeoutWithCrossGroupFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	profile := RequestProfile{
		RequestType: RequestTypeChatLongStream,
		Protocol:    string(types.RelayFormatOpenAIResponses),
		IsStream:    true,
	}
	setRequestProfile(context, profile)
	budget := StartRequestBudget(context, profile, time.Now())
	require.True(t, budget.TryBeginAttempt(time.Now(), "provider:a"))
	MarkSingleChannelRoute(context, true)
	MarkRemainingCrossGroupRoutes(context, 1)

	require.Equal(t, responsesFirstAttemptWaitTimeout, RetryableResponsesAttemptTimeout(context))
}

func TestResponsesShortStreamUsesAdaptiveTTFTTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	profile := RequestProfile{
		RequestType: RequestTypeChatShortStream,
		Protocol:    string(types.RelayFormatOpenAIResponses),
		IsStream:    true,
	}
	setRequestProfile(context, profile)
	StartRequestBudget(context, profile, time.Now())
	context.Set(string(constant.ContextKeyChannelId), 912_345)
	context.Set(string(constant.ContextKeyOriginalModel), "gpt-adaptive-timeout")
	for sample := 0; sample < responsesAdaptiveTTFTMinSamples; sample++ {
		RecordChannelSuccess(912_345, "gpt-adaptive-timeout", 16*time.Second, RequestTypeChatShortStream)
	}

	require.Equal(t, 20*time.Second, RetryableResponsesAttemptTimeout(context))
}

func TestResponsesShortStreamBucketsAdaptiveTimeoutForConnectionReuse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	profile := RequestProfile{
		RequestType: RequestTypeChatShortStream,
		Protocol:    string(types.RelayFormatOpenAIResponses),
		IsStream:    true,
	}
	setRequestProfile(context, profile)
	StartRequestBudget(context, profile, time.Now())
	context.Set(string(constant.ContextKeyChannelId), 912_348)
	context.Set(string(constant.ContextKeyOriginalModel), "gpt-adaptive-bucket")
	for sample := 0; sample < responsesAdaptiveTTFTMinSamples; sample++ {
		RecordChannelSuccess(912_348, "gpt-adaptive-bucket", 17*time.Second, RequestTypeChatShortStream)
	}

	// 17s * 1.25 = 21.25s. Round up to a stable transport bucket so nearby
	// channel percentiles share an outbound connection pool.
	require.Equal(t, 25*time.Second, RetryableResponsesAttemptTimeout(context))
}

func TestResponsesShortStreamUsesConservativeDefaultWithoutSamples(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	profile := RequestProfile{
		RequestType: RequestTypeChatShortStream,
		Protocol:    string(types.RelayFormatOpenAIResponses),
		IsStream:    true,
	}
	setRequestProfile(context, profile)
	StartRequestBudget(context, profile, time.Now())

	require.Equal(t, responsesShortAttemptDefault, RetryableResponsesAttemptTimeout(context))
}

func TestResponsesShortStreamClampsAdaptiveTTFTTimeout(t *testing.T) {
	testCases := []struct {
		name      string
		channelID int
		model     string
		ttft      time.Duration
		expected  time.Duration
	}{
		{name: "minimum", channelID: 912_346, model: "gpt-adaptive-min", ttft: time.Second, expected: responsesShortAttemptMin},
		{name: "maximum", channelID: 912_347, model: "gpt-adaptive-max", ttft: 40 * time.Second, expected: responsesShortAttemptMax},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			profile := RequestProfile{
				RequestType: RequestTypeChatShortStream,
				Protocol:    string(types.RelayFormatOpenAIResponses),
				IsStream:    true,
			}
			setRequestProfile(context, profile)
			StartRequestBudget(context, profile, time.Now())
			context.Set(string(constant.ContextKeyChannelId), testCase.channelID)
			context.Set(string(constant.ContextKeyOriginalModel), testCase.model)
			for sample := 0; sample < responsesAdaptiveTTFTMinSamples; sample++ {
				RecordChannelSuccess(testCase.channelID, testCase.model, testCase.ttft, RequestTypeChatShortStream)
			}

			require.Equal(t, testCase.expected, RetryableResponsesAttemptTimeout(context))
		})
	}
}

func TestSpecificChannelDoesNotUseFastResponsesRetryWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	profile := RequestProfile{
		RequestType: RequestTypeChatLongStream,
		Protocol:    string(types.RelayFormatOpenAIResponses),
		IsStream:    true,
	}
	setRequestProfile(context, profile)
	StartRequestBudget(context, profile, time.Now())
	context.Set("specific_channel_id", 6)

	require.Zero(t, RetryableResponsesAttemptTimeout(context))
}

func TestRemainingCrossGroupRouteState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	require.False(t, HasRemainingCrossGroupRoute(context))
	MarkRemainingCrossGroupRoutes(context, 2)
	require.True(t, HasRemainingCrossGroupRoute(context))
	MarkRemainingCrossGroupRoutes(context, 0)
	require.False(t, HasRemainingCrossGroupRoute(context))
}
