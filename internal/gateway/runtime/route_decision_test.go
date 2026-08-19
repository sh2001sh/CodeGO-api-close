package runtime

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
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

func TestRouteDecisionRecordsAutoCandidateLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set(constant.RequestIdKey, "candidate-test")
	StartRouteDecision(context, "gpt-test", "auto")
	RecordRouteDecisionCandidate(context, 1, "group-a", "selected", "preflight_ok", 42)
	StartRouteDecisionAttempt(context, 0, 42, "provider:a")
	FinishRouteDecisionAttempt(context, false, 502, "transient", "bootstrap")
	RecordRouteDecisionCandidate(context, 2, "group-b", "skipped", "no_healthy_channel", 0)

	decision, found := GetRouteDecision(context)
	require.True(t, found)
	require.Len(t, decision.Candidates, 2)
	require.Equal(t, "attempted", decision.Candidates[0].Status)
	require.Equal(t, "transient", decision.Candidates[0].Reason)
	require.Equal(t, "skipped", decision.Candidates[1].Status)
	require.Equal(t, "no_healthy_channel", decision.Candidates[1].Reason)
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

func TestAttachRouteLogInfoBuildsIdentifierFreeAutoSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set(constant.RequestIdKey, "route-log-test")
	StartRouteDecision(context, "gpt-test", "auto")
	MarkAutoRouteRequest(context)
	httpctx.SetContextKey(context, constant.ContextKeyUnifiedAutoBindings, []string{"internal-a", "internal-b", "internal-c"})
	httpctx.SetContextKey(context, constant.ContextKeyUnifiedAutoIndex, 2)
	UpdateRouteDecisionCandidates(context, 3)
	ExcludeRouteDecisionCandidate(context, "marketplace_auto_unavailable")
	SelectRouteDecisionCandidate(context, "internal-c", 42, false)

	other := make(map[string]interface{})
	AttachRouteLogInfo(context, other)

	summary, ok := other[routeSummaryLogKey].(RouteLogSummary)
	require.True(t, ok)
	require.Equal(t, 3, summary.CandidateCount)
	require.Equal(t, 3, summary.SelectedOrder)
	require.Equal(t, 2, summary.SkippedCount)
	require.True(t, summary.Fallback)
	require.Equal(t, []string{"unavailable"}, summary.SkipReasons)
	require.NotContains(t, fmt.Sprint(summary), "internal-c")
	require.NotContains(t, fmt.Sprint(summary), "42")

	adminInfo, ok := other[adminInfoLogKey].(map[string]interface{})
	require.True(t, ok)
	_, ok = adminInfo["route_decision"].(RouteDecision)
	require.True(t, ok)
}

func TestAttachRouteLogInfoCountsExhaustedCandidates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	StartRouteDecision(context, "gpt-test", "auto")
	MarkAutoRouteRequest(context)
	httpctx.SetContextKey(context, constant.ContextKeyUnifiedAutoBindings, []string{"a", "b"})
	httpctx.SetContextKey(context, constant.ContextKeyUnifiedAutoIndex, -1)
	UpdateRouteDecisionCandidates(context, 2)

	other := make(map[string]interface{})
	AttachRouteLogInfo(context, other)
	summary := other[routeSummaryLogKey].(RouteLogSummary)
	require.Zero(t, summary.SelectedOrder)
	require.Equal(t, 2, summary.SkippedCount)
}
