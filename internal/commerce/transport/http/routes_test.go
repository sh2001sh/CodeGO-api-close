package http

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterCommerceRoutesRegistersWalletQuotaConversionMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterCommerceRoutes(router.Group("/api"), func(c *gin.Context) { c.Next() })

	routes := make(map[string]struct{})
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}
	require.Contains(t, routes, http.MethodGet+" /api/wallet/quota-conversions")
	require.Contains(t, routes, http.MethodPost+" /api/wallet/quota-conversions")
}
