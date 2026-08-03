package runtime

import (
	"net/http/httptest"
	"testing"

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

	SetRouteDecisionProbeMode(context, "invalid")
	decision, found = GetRouteDecision(context)
	require.True(t, found)
	require.Equal(t, RouteDecisionProbeLastResort, decision.ProbeMode)
}
