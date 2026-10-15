package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/stretchr/testify/require"
)

func TestMarketplacePoolSelectionGroupUsesNamedPoolTokenGroup(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	httpctx.SetContextKey(ctx, constant.ContextKeyTokenGroup, "market:pool:named-pool")

	require.Equal(t, "market:pool:named-pool", marketplacePoolSelectionGroup(ctx, "market:auto"))
}

func TestMarketplacePoolSelectionGroupKeepsCurrentGroupForAuto(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	httpctx.SetContextKey(ctx, constant.ContextKeyTokenGroup, "market:auto")

	require.Equal(t, "market:auto", marketplacePoolSelectionGroup(ctx, "market:auto"))
}
