package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCommunityBridgeRoutesRequireDedicatedServiceSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CODEGO_COMMUNITY_API_SECRET", "community-test-secret-with-at-least-32-characters")
	engine := gin.New()
	RegisterCommunityRoutes(engine.Group("/api"))

	for _, authorization := range []string{"", "Bearer wrong-secret-with-at-least-32-characters"} {
		request := httptest.NewRequest(http.MethodGet, "/api/community/v1/members/ABC234", nil)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		require.Equal(t, http.StatusUnauthorized, response.Code)
		var body struct {
			Code string `json:"code"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.Equal(t, "UNAUTHORIZED", body.Code)
	}
}

func TestCommunityBridgeReturnsUnavailableWhenServiceSecretIsNotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CODEGO_COMMUNITY_API_SECRET", "")
	engine := gin.New()
	RegisterCommunityRoutes(engine.Group("/api"))
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/community/v1/members/ABC234", nil))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "COMMUNITY_API_DISABLED", body.Code)
}

func TestCommunityBridgeRoutesAreVersionedAndRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterCommunityRoutes(engine.Group("/api"))
	want := map[string]bool{
		"GET /api/community/v1/sellers":               false,
		"GET /api/community/v1/members/:sub":          false,
		"GET /api/community/v1/members/:sub/channels": false,
		"PUT /api/community/v1/channels/:id/rating":   false,
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

func TestCommunityBridgeUsesHTTPStatusAndLimitedMemberPayload(t *testing.T) {
	originalDB := platformdb.DB
	db, err := gorm.Open(sqlite.Open("file:community-bridge-http?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &marketplaceschema.Channel{}, &marketplaceschema.Group{}))
	require.NoError(t, db.Create(&identityschema.User{
		ExternalId: "ABC234", Username: "member", DisplayName: "Member", Email: "private@example.com",
		Password: "unused-password", Quota: 123456, Status: constant.UserStatusEnabled,
	}).Error)

	secret := "community-http-secret-with-at-least-32-characters"
	t.Setenv("CODEGO_COMMUNITY_API_SECRET", secret)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterCommunityRoutes(engine.Group("/api"))

	request := httptest.NewRequest(http.MethodGet, "/api/community/v1/members/ABC234", nil)
	request.Header.Set("Authorization", "Bearer "+secret)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.NotContains(t, response.Body.String(), "private@example.com")
	require.NotContains(t, response.Body.String(), "123456")
	require.NotContains(t, response.Body.String(), "password")

	invalid := httptest.NewRequest(http.MethodGet, "/api/community/v1/members/invalid", nil)
	invalid.Header.Set("Authorization", "Bearer "+secret)
	invalidResponse := httptest.NewRecorder()
	engine.ServeHTTP(invalidResponse, invalid)
	require.Equal(t, http.StatusBadRequest, invalidResponse.Code)

	missing := httptest.NewRequest(http.MethodGet, "/api/community/v1/members/DEF567", nil)
	missing.Header.Set("Authorization", "Bearer "+secret)
	missingResponse := httptest.NewRecorder()
	engine.ServeHTTP(missingResponse, missing)
	require.Equal(t, http.StatusNotFound, missingResponse.Code)
}

func TestCommunityBridgeRejectsInvalidPaginationBeforeDatabaseAccess(t *testing.T) {
	secret := "community-pagination-secret-with-at-least-32-characters"
	t.Setenv("CODEGO_COMMUNITY_API_SECRET", secret)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterCommunityRoutes(engine.Group("/api"))
	request := httptest.NewRequest(http.MethodGet, "/api/community/v1/members/ABC234/channels?page=0&page_size=100", nil)
	request.Header.Set("Authorization", "Bearer "+secret)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "INVALID_PAGINATION", body.Code)
}

func TestCommunityBridgeRejectsInvalidRatingBeforeDatabaseAccess(t *testing.T) {
	secret := "community-rating-secret-with-at-least-32-characters"
	t.Setenv("CODEGO_COMMUNITY_API_SECRET", secret)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterCommunityRoutes(engine.Group("/api"))
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/community/v1/channels/channel-1/rating",
		strings.NewReader(`{"viewer_sub":"ABC234","stars":6}`),
	)
	request.Header.Set("Authorization", "Bearer "+secret)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "INVALID_RATING", body.Code)
}
