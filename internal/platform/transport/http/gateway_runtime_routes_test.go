package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
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
		"GET /v1/sub2api/billing",
	} {
		require.True(t, routes[route], route)
	}
}

func TestSignedDeliveryRouteUsesConfiguredGlobalLimitWithoutBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldEnabled, oldNum, oldDuration, oldRedis := platformconfig.GlobalApiRateLimitEnable, platformconfig.GlobalApiRateLimitNum, platformconfig.GlobalApiRateLimitDuration, platformcache.RedisEnabled
	platformconfig.GlobalApiRateLimitEnable, platformconfig.GlobalApiRateLimitNum, platformconfig.GlobalApiRateLimitDuration, platformcache.RedisEnabled = true, 1, 60, false
	t.Cleanup(func() {
		platformconfig.GlobalApiRateLimitEnable, platformconfig.GlobalApiRateLimitNum, platformconfig.GlobalApiRateLimitDuration, platformcache.RedisEnabled = oldEnabled, oldNum, oldDuration, oldRedis
	})
	engine := gin.New()
	registerRelayCoreRoutes(engine)
	for _, want := range []int{http.StatusNotFound, http.StatusTooManyRequests} {
		r := httptest.NewRequest(http.MethodGet, "/v1/files/file-codego-no-signature/delivery", nil)
		r.RemoteAddr = "203.0.113.129:12345"
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		require.Equal(t, want, w.Code)
	}
}
