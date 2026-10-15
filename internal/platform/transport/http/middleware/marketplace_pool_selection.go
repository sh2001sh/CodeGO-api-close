package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

// Authentication uses market:auto as the temporary routing group for every
// marketplace pool. Keep a named pool's persisted token group for resolution.
func marketplacePoolSelectionGroup(c *gin.Context, usingGroup string) string {
	tokenGroup := httpctx.GetContextKeyString(c, constant.ContextKeyTokenGroup)
	if marketplaceapp.IsMarketplaceRoutePoolTokenGroup(tokenGroup) {
		return tokenGroup
	}
	return usingGroup
}
