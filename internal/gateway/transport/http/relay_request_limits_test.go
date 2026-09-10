package http

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceUserConcurrencyLimitHonorsUserBypass(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.Equal(t, 3, marketplaceUserConcurrencyLimit(ctx, 3))

	httpctx.SetContextKey(ctx, constant.ContextKeyBypassModelRequestLimits, true)
	require.Zero(t, marketplaceUserConcurrencyLimit(ctx, 3))
}
