package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIncomeReclaimRejectsInvalidAmountsBeforeAccessingBalances(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/reclaim", ReleaseAdminOwnerIncome)
	for _, query := range []string{
		"max_amount=", "max_amount=-1", "max_amount=0", "max_amount=nope", "max_amount=1.5",
		"max_amount=9223372036854775808", "max_amount=40",
	} {
		t.Run(query, func(t *testing.T) {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/reclaim?owner_user_ids=10&"+query, nil))
			var body struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.False(t, body.Success)
			require.NotEmpty(t, body.Message)
		})
	}
}

func TestMarketplaceRoutesRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("marketplace-route-test"))))
	RegisterMarketplaceRoutes(engine.Group("/api"))

	for _, target := range []string{
		"/api/marketplace/auto-route-pool",
		"/api/marketplace/route-pools",
		"/api/marketplace/channels/mine",
		"/api/marketplace/channels/mine/logs",
		"/api/marketplace/channels/mine/logs/export",
		"/api/marketplace/security-audit/events/export",
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
		"GET /api/marketplace/groups":                       false,
		"GET /api/marketplace/multiplier-trends":            false,
		"GET /api/marketplace/auto-route-pool":              false,
		"PUT /api/marketplace/auto-route-pool":              false,
		"GET /api/marketplace/route-pools":                  false,
		"POST /api/marketplace/route-pools":                 false,
		"GET /api/marketplace/route-pools/:id":              false,
		"PUT /api/marketplace/route-pools/:id":              false,
		"DELETE /api/marketplace/route-pools/:id":           false,
		"POST /api/marketplace/batch-tests":                 false,
		"GET /api/marketplace/batch-tests/:id":              false,
		"POST /api/marketplace/groups/:id/bind-token":       false,
		"POST /api/marketplace/groups/:id/invite":           false,
		"POST /api/marketplace/invites/accept":              false,
		"POST /api/marketplace/channels":                    false,
		"POST /api/marketplace/channels/fetch-models":       false,
		"POST /api/marketplace/channels/:id/detect":         false,
		"POST /api/marketplace/channels/:id/test":           false,
		"GET /api/marketplace/channels/mine/logs":           false,
		"GET /api/marketplace/channels/mine/logs/export":    false,
		"GET /api/marketplace/security-audit/events/export": false,
		"GET /api/marketplace/channels/mine/observability":  false,
		"PATCH /api/marketplace/admin/channels/:id":         false,
		"POST /api/marketplace/admin/channels/:id/detect":   false,
		"POST /api/marketplace/admin/channels/:id/test":     false,
		"DELETE /api/marketplace/channels/:id":              false,
		"DELETE /api/marketplace/admin/channels/:id":        false,
		"POST /api/marketplace/admin/channels/:id/review":   false,
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

func TestIncomeReclaimRejectsFiltersThatWouldWidenSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/reclaim", ReleaseAdminOwnerIncome)
	for _, query := range []string{"", "owner_user_ids=", "owner_user_ids=nope", "owner_user_ids=0", "owner_user_ids=10,nope", "owner_user_ids=10&start_timestamp=oops", "owner_user_ids=10&end_timestamp=-1", "owner_user_ids=10&start_timestamp=100&end_timestamp=50"} {
		t.Run(query, func(t *testing.T) {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/reclaim?"+query, nil))
			var body struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.False(t, body.Success)
			require.NotEmpty(t, body.Message)
		})
	}
}
