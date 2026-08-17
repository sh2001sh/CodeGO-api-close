package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceRoutesRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("marketplace-route-test"))))
	RegisterMarketplaceRoutes(engine.Group("/api"))

	for _, target := range []string{
		"/api/marketplace/auto-route-pool",
		"/api/marketplace/channels/mine",
		"/api/marketplace/channels/mine/logs",
		"/api/marketplace/admin/channels",
	} {
		t.Run(target, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, target, nil)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, http.StatusUnauthorized, response.Code)
		})
	}
}

func TestMarketplaceRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterMarketplaceRoutes(engine.Group("/api"))

	want := map[string]bool{
		"GET /api/marketplace/groups":                     false,
		"GET /api/marketplace/multiplier-trends":          false,
		"GET /api/marketplace/auto-route-pool":            false,
		"PUT /api/marketplace/auto-route-pool":            false,
		"POST /api/marketplace/groups/:id/bind-token":     false,
		"POST /api/marketplace/channels":                  false,
		"POST /api/marketplace/channels/fetch-models":     false,
		"POST /api/marketplace/channels/:id/detect":       false,
		"POST /api/marketplace/channels/:id/test":         false,
		"GET /api/marketplace/channels/mine/logs":         false,
		"PATCH /api/marketplace/admin/channels/:id":       false,
		"POST /api/marketplace/admin/channels/:id/detect": false,
		"POST /api/marketplace/admin/channels/:id/test":   false,
		"DELETE /api/marketplace/channels/:id":            false,
		"DELETE /api/marketplace/admin/channels/:id":      false,
		"POST /api/marketplace/admin/channels/:id/review": false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, exists := want[key]; exists {
			want[key] = true
		}
	}
	for route, registered := range want {
		require.Truef(t, registered, "route %s was not registered", route)
	}
}
