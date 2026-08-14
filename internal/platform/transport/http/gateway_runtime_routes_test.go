package http

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayRuntimeRegistersCodexCompatibilityRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterGatewayRuntimeRoutes(router)

	routes := make(map[string]bool)
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /v1/responses",
		"GET /responses",
		"GET /backend-api/codex/responses",
		"POST /v1/alpha/search",
		"POST /alpha/search",
		"POST /responses",
		"POST /responses/compact",
		"POST /backend-api/codex/alpha/search",
		"POST /backend-api/codex/responses",
		"POST /backend-api/codex/responses/compact",
	} {
		require.True(t, routes[route], route)
	}
}
