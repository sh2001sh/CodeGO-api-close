package runtime

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
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

func TestRequestBudgetStartTimeExcludesLargeUpload(t *testing.T) {
	requestStarted := time.Unix(100, 0)
	validated := requestStarted.Add(90 * time.Second)
	if got := RequestBudgetStartTime(requestStarted, validated, (8<<20)-1); !got.Equal(requestStarted) {
		t.Fatalf("small body should retain request start: got %v", got)
	}
	if got := RequestBudgetStartTime(requestStarted, validated, 8<<20); !got.Equal(validated) {
		t.Fatalf("large body should start budget after validation: got %v", got)
	}
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
	require.True(t, budget.CanRetry(now.Add(time.Second)))
	require.True(t, budget.TryBeginAttempt(now.Add(time.Second), "provider:a"))
	require.False(t, budget.CanRetry(now.Add(2*time.Second)))
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

func TestAutomaticRouteFirstByteTimeoutDoesNotReplayAcceptedWork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	profile := RequestProfile{
		RequestType:         RequestTypeChatShortStream,
		IsStream:            true,
		MigrationCapability: MigrationUnbound,
	}
	setRequestProfile(context, profile)
	MarkAutoRouteRequest(context)
	MarkRemainingCrossGroupRoutes(context, 1)
	budget := StartRequestBudget(context, profile, time.Now())
	require.True(t, budget.TryBeginAttempt(time.Now(), "provider:a"))

	require.Zero(t, AutomaticRouteFirstByteTimeout(context))

	context.Set("specific_channel_id", 9)
	require.Zero(t, AutomaticRouteFirstByteTimeout(context))
	context.Set("specific_channel_id", nil)
	MarkRemainingCrossGroupRoutes(context, 0)
	require.Zero(t, AutomaticRouteFirstByteTimeout(context))
}

func TestAutomaticRouteFirstByteTimeoutProtectsUpstreamState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	profile := RequestProfile{
		RequestType:         RequestTypeChatLongStream,
		IsStream:            true,
		MigrationCapability: MigrationUpstreamStateBound,
	}
	setRequestProfile(context, profile)
	MarkAutoRouteRequest(context)
	MarkRemainingCrossGroupRoutes(context, 1)
	budget := StartRequestBudget(context, profile, time.Now())
	require.True(t, budget.TryBeginAttempt(time.Now(), "provider:a"))

	require.Zero(t, AutomaticRouteFirstByteTimeout(context))
}

func TestAutomaticRouteFirstByteTimeoutStaysDisabledAcrossProfiles(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	profile := RequestProfile{RequestType: RequestTypeChatLongStream, IsStream: true, MigrationCapability: MigrationCacheAffinity, PromptSizeBucket: PromptSizeMedium}
	setRequestProfile(ctx, profile)
	MarkAutoRouteRequest(ctx)
	MarkRemainingCrossGroupRoutes(ctx, 1)
	MarkLongContextRequestWithContinuation(ctx, "gpt-5.6-sol", 85000, true)
	budget := StartRequestBudget(ctx, profile, time.Now())
	require.True(t, budget.TryBeginAttempt(time.Now(), "provider:a"))
	require.Zero(t, AutomaticRouteFirstByteTimeout(ctx))
	profile.PromptSizeBucket = PromptSizeVeryLarge
	setRequestProfile(ctx, profile)
	require.Zero(t, AutomaticRouteFirstByteTimeout(ctx))
	profile.RequestType = RequestTypeToolCallStream
	setRequestProfile(ctx, profile)
	require.Zero(t, AutomaticRouteFirstByteTimeout(ctx))
}

func TestAutomaticFirstByteWaitDoesNotCreateSpeculativeDeadline(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	profile := RequestProfile{RequestType: RequestTypeChatLongStream, IsStream: true, MigrationCapability: MigrationCacheAffinity}
	setRequestProfile(ctx, profile)
	MarkAutoRouteRequest(ctx)
	MarkRemainingCrossGroupRoutes(ctx, 1)
	StartRequestBudget(ctx, profile, time.Now())
	StartRouteDecision(ctx, "gpt-5.6-sol", "auto")
	StartRouteDecisionAttempt(ctx, 0, 624217, "first")
	require.Zero(t, StartAutomaticFirstByteWait(ctx))
	require.Zero(t, StreamFirstOutputTimeoutForRequest(ctx, "gpt-5.6-sol", 85000))
	MarkRemainingCrossGroupRoutes(ctx, 0)
	StartRouteDecisionAttempt(ctx, 1, 624218, "last")
	require.Zero(t, StartAutomaticFirstByteWait(ctx))
	require.Zero(t, RemainingAutomaticFirstByteWait(ctx), "last attempt must not inherit the previous deadline")
	decision, ok := GetRouteDecision(ctx)
	require.True(t, ok)
	require.Zero(t, decision.Attempts[0].FirstByteTimeoutMS)
	require.Zero(t, decision.Attempts[1].FirstByteTimeoutMS)
}
