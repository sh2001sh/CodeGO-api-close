package runtime

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestSetRouteDecisionProbeMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set(constant.RequestIdKey, "probe-mode-test")
	StartRouteDecision(context, "gpt-test", "auto")

	SetRouteDecisionProbeMode(context, RouteDecisionProbeLastResort)
	decision, found := GetRouteDecision(context)
	require.True(t, found)
	require.Equal(t, RouteDecisionProbeLastResort, decision.ProbeMode)
	SetRouteDecisionProbeMode(context, RouteDecisionProbeRateLimit)
	decision, found = GetRouteDecision(context)
	require.True(t, found)
	require.Equal(t, RouteDecisionProbeRateLimit, decision.ProbeMode)

	SetRouteDecisionProbeMode(context, "invalid")
	decision, found = GetRouteDecision(context)
	require.True(t, found)
	require.Equal(t, RouteDecisionProbeRateLimit, decision.ProbeMode)
}

func TestRouteDecisionRecordsAttemptLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set(constant.RequestIdKey, "attempt-test")
	InitializeRequestProfile(context, "gpt-test", "/v1/responses", RequestProfileHint{IsStream: true})
	StartRouteDecision(context, "gpt-test", "auto")
	budget := StartRequestBudget(context, RequestProfile{RequestType: RequestTypeChatShortStream}, time.Now())
	require.True(t, budget.TryBeginAttempt(time.Now(), "provider:a"))
	UpdateRouteDecisionBudget(context, budget)
	StartRouteDecisionAttempt(context, 0, 42, "provider:a")
	FinishRouteDecisionAttempt(context, false, 502, "transient", "bootstrap")

	decision, found := GetRouteDecision(context)
	require.True(t, found)
	require.Equal(t, 1, decision.AttemptsUsed)
	require.Len(t, decision.Attempts, 1)
	require.Equal(t, "attempt-test:1", decision.Attempts[0].AttemptID)
	require.Equal(t, 42, decision.Attempts[0].ChannelID)
	require.Equal(t, "transient", decision.Attempts[0].FailureClass)
	require.Equal(t, "bootstrap", decision.Attempts[0].Stage)
}

func TestFinishRouteDecisionAttemptWithoutStartedAttemptIsNoOp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set(constant.RequestIdKey, "no-attempt-test")
	StartRouteDecision(context, "gpt-test", "auto")

	FinishRouteDecisionAttempt(context, false, 503, "transient", "bootstrap")

	decision, found := GetRouteDecision(context)
	require.True(t, found)
	require.Empty(t, decision.Attempts)
	require.Zero(t, decision.AttemptsUsed)
}
