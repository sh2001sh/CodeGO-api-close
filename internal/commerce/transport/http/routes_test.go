package http

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterCommerceRoutesKeepsUnifiedWalletAndMonthlyPassWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterCommerceRoutes(router.Group("/api"), func(c *gin.Context) { c.Next() })

	routes := make(map[string]struct{})
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}
	require.NotContains(t, routes, http.MethodGet+" /api/wallet/quota-conversions")
	require.NotContains(t, routes, http.MethodPost+" /api/wallet/quota-conversions")
	require.Contains(t, routes, http.MethodPost+" /api/subscription/self/claude-conversions")
	require.NotContains(t, routes, http.MethodPost+" /api/blind-box/open")
	require.Contains(t, routes, http.MethodPut+" /api/subscription/self/preference")
	require.Contains(t, routes, http.MethodPost+" /api/subscription/self/reset-opportunity/use")
	require.Contains(t, routes, http.MethodPost+" /api/packages/purchase")
	require.Contains(t, routes, http.MethodPost+" /api/group-buy/join")
	require.Contains(t, routes, http.MethodPost+" /api/blind-box/inventory/open")
	require.Contains(t, routes, http.MethodPost+" /api/blind-box/orders/:trade_no/cancel")
	require.Contains(t, routes, http.MethodPost+" /api/blind-box/simulation/draw")
	require.Contains(t, routes, http.MethodPost+" /api/wallet/transfers/payment-password/email-code")
}
