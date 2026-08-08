package runtime

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
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
