package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	requestsettings "github.com/sh2001sh/new-api/internal/platform/requestsettings"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/stretchr/testify/require"
)

func TestModelRequestRateLimitBypassRunsHandler(t *testing.T) {
	previous := requestsettings.ModelRequestRateLimitEnabled
	requestsettings.ModelRequestRateLimitEnabled = true
	t.Cleanup(func() { requestsettings.ModelRequestRateLimitEnabled = previous })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	httpctx.SetContextKey(ctx, constant.ContextKeyBypassModelRequestLimits, true)
	called := false
	modelRequestRateLimitWithHandler(func(*gin.Context) { called = true })(ctx)
	require.True(t, called)
}
