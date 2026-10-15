package runtime

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/stretchr/testify/require"
)

func TestRouteLogIdentityKeepsPoolNameAndFinalGroupSeparate(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	MarkAutoRouteRequest(c)
	StartRouteDecision(c, "model", "market:auto")
	httpctx.SetContextKey(c, constant.ContextKeyUsingGroup, "market:auto")
	other := map[string]interface{}{}
	AttachRouteLogInfo(c, other)
	require.Equal(t, "自动路由池", other["route_pool_name"])
	require.NotContains(t, other, "actual_group")

	c.Set(RoutePoolNameContextKey, "Named Pool")
	SelectRouteDecisionCandidate(c, "first-group", 1, false)
	SelectRouteDecisionCandidate(c, "final-group", 2, false)
	httpctx.SetContextKey(c, constant.ContextKeyUsingGroup, "final-group")
	AttachRouteLogInfo(c, other)
	require.Equal(t, "Named Pool", other["route_pool_name"])
	require.Equal(t, "final-group", other["actual_group"])
	require.True(t, other[routeSummaryLogKey].(RouteLogSummary).Fallback)
}
