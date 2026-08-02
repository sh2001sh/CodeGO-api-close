package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestRegisterBountyRoutesIncludesUserAndAdminContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api")
	RegisterBountyRoutes(api)

	routes := make(map[string]struct{})
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	for _, route := range []string{
		"GET /api/bounties",
		"POST /api/bounties",
		"POST /api/bounties/drafts",
		"PUT /api/bounties/:id/draft",
		"POST /api/bounties/:id/draft/publish",
		"GET /api/bounties/mine",
		"GET /api/bounties/balances",
		"GET /api/bounties/notifications",
		"POST /api/bounties/notifications/read-all",
		"GET /api/bounties/:id",
		"GET /api/bounties/:id/timeline",
		"POST /api/bounties/:id/applications",
		"POST /api/bounties/:id/assignment",
		"POST /api/bounties/:id/material-requests",
		"POST /api/bounties/:id/material-requests/:request_id/replies",
		"POST /api/bounties/:id/material-requests/:request_id/resolve",
		"POST /api/bounties/:id/material-requests/:request_id/timeout",
		"POST /api/bounties/:id/submissions",
		"POST /api/bounties/:id/review",
		"POST /api/bounties/:id/disputes",
		"POST /api/bounties/:id/reports",
		"POST /api/bounties/:id/cancel",
		"GET /api/admin/bounties",
		"GET /api/admin/bounties/disputes",
		"GET /api/admin/bounties/reports",
		"POST /api/admin/bounties/:id/resolve",
		"POST /api/admin/bounties/:id/suspend",
		"POST /api/admin/bounties/:id/resume",
		"POST /api/admin/bounties/:id/reports/:report_id/resolve",
	} {
		require.Contains(t, routes, route)
	}
}

func TestBountyUserAndAdminRoutesRequireExpectedAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("bounty-route-test"))))
	api := router.Group("/api")
	RegisterBountyRoutes(api)

	request := httptest.NewRequest(http.MethodGet, "/api/bounties/mine", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	adminRequest := httptest.NewRequest(http.MethodGet, "/api/admin/bounties", nil)
	adminRecorder := httptest.NewRecorder()
	router.ServeHTTP(adminRecorder, adminRequest)
	require.Equal(t, http.StatusUnauthorized, adminRecorder.Code)
}

func TestBountyCreateRejectsMalformedJSONAfterAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("bounty-body-test"))))
	router.POST("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("username", "bounty-user")
		session.Set("role", constant.RoleCommonUser)
		session.Set("id", 1)
		session.Set("status", constant.UserStatusEnabled)
		session.Set("group", "default")
		require.NoError(t, session.Save())
	})
	api := router.Group("/api")
	RegisterBountyRoutes(api)

	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodPost, "/login", nil))
	request := httptest.NewRequest(http.MethodPost, "/api/bounties", bytes.NewBufferString("{"))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("New-Api-User", "1")
	for _, cookie := range loginRecorder.Result().Cookies() {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestFirstNonEmptyUsesFirstTrimmedValue(t *testing.T) {
	require.Equal(t, "header-key", firstNonEmpty(" ", " header-key ", "body-key"))
	require.Empty(t, firstNonEmpty(" ", "\t"))
}
