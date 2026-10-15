package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
)

func TestGetSub2APIKeyBillingReturnsAuthenticatedTokenGroup(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	user := &identityschema.User{
		Id: 71, Username: "sub2api-user", Password: "password123", DisplayName: "Sub2API User",
		Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default", AffCode: "SUB2API71",
	}
	require.NoError(t, db.Create(user).Error)
	token := &identityschema.Token{
		Id: 72, UserId: user.Id, Key: "sub2api-key", Name: "sub2api", Status: constant.TokenStatusEnabled,
		CreatedTime: 1, AccessedTime: 1, ExpiredTime: -1, UnlimitedQuota: true, Group: "default",
	}
	require.NoError(t, db.Create(token).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/sub2api/billing", nil)
	ctx.Set("id", user.Id)
	ctx.Set("token_id", token.Id)

	GetSub2APIKeyBilling(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.JSONEq(t, `{"object":"sub2api.key_billing","schema_version":1,"billing_scope":"token","group":"default","group_rate_multiplier":1,"resolved_rate_multiplier":1,"effective_rate_multiplier":1}`, recorder.Body.String())
}

func TestGetSub2APIKeyBillingRejectsMissingTokenContext(t *testing.T) {
	setupModelListControllerTestDB(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/sub2api/billing", nil)

	GetSub2APIKeyBilling(ctx)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.JSONEq(t, `{"error":{"type":"authentication_error","message":"Invalid API key"}}`, recorder.Body.String())
}
