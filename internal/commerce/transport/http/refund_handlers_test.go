package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
)

func TestRefundRoutesRequireAdministratorAndRetireUserEndpoints(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	users := []identityschema.User{
		{Id: 1, Username: "refund-admin", Role: constant.RoleAdminUser, Status: constant.UserStatusEnabled},
		{Id: 2, Username: "refund-user", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled},
		{Id: 3, Username: "refund-disabled-admin", Role: constant.RoleAdminUser, Status: constant.UserStatusDisabled},
	}
	for i := range users {
		users[i].AffCode = "refund-" + strconv.Itoa(users[i].Id)
		require.NoError(t, db.Create(&users[i]).Error)
	}
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("refund-routes-test-secret"))))
	// Only the test harness can create these sessions; auth still reloads live roles.
	router.GET("/test/session/:id", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Param("id"))
		user := users[id-1]
		session := sessions.Default(c)
		session.Set("id", id)
		session.Set("username", user.Username)
		session.Set("role", constant.RoleAdminUser) // A stale or forged role cannot grant access.
		session.Set("status", constant.UserStatusEnabled)
		require.NoError(t, session.Save())
	})
	RegisterCommerceRoutes(router.Group("/api"), func(c *gin.Context) { c.Next() })
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/api/wallet/refunds/eligible"},
		{http.MethodPost, "/api/wallet/refunds"},
		{http.MethodPost, "/api/wallet/refunds/old-refund/sync"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(endpoint.method, endpoint.path, nil))
		require.Equal(t, http.StatusNotFound, recorder.Code, endpoint.path)
	}
	for _, userID := range []int{2, 3, 1} {
		login := httptest.NewRecorder()
		router.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/test/session/"+strconv.Itoa(userID), nil))
		for _, endpoint := range []struct{ method, path string }{
			{http.MethodGet, "/api/wallet/admin/users/2/refunds/eligible"},
			{http.MethodPost, "/api/wallet/admin/users/2/refunds"},
			{http.MethodPost, "/api/wallet/admin/users/2/refunds/unknown/sync"},
		} {
			if userID == 1 && endpoint.method != http.MethodGet {
				continue // The successful financial call is covered at the service boundary.
			}
			request := httptest.NewRequest(endpoint.method, endpoint.path, bytes.NewBufferString(`{"order_type":"balance","trade_no":"unknown"}`))
			request.Header.Set("Cookie", login.Header().Get("Set-Cookie"))
			request.Header.Set("New-Api-User", strconv.Itoa(userID))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			response := decodeCommerceResponse(t, recorder)
			require.Equal(t, userID == 1, response.Success, "user %d %s: %s", userID, endpoint.path, recorder.Body.String())
		}
	}
}

func TestRefundServiceRejectsNonAdminAndWrongOrderOwner(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	for _, user := range []identityschema.User{
		{Id: 1, Username: "refund-service-admin", Role: constant.RoleAdminUser, Status: constant.UserStatusEnabled},
		{Id: 2, Username: "refund-service-user", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled},
		{Id: 3, Username: "refund-service-owner", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled},
	} {
		user.AffCode = "refund-" + strconv.Itoa(user.Id)
		require.NoError(t, db.Create(&user).Error)
	}
	require.NoError(t, db.Create(&commerceschema.TopUp{UserId: 3, TradeNo: "other-owner", Status: constant.TopUpStatusSuccess}).Error)
	request := commerceschema.RefundRequest{OrderType: "balance", TradeNo: "other-owner"}
	_, err := commerceapp.CreateAdminRefund(2, 3, request)
	require.ErrorIs(t, err, commerceapp.ErrRefundAdminRequired)
	_, err = commerceapp.ListAdminRefundableOrders(2, 3)
	require.ErrorIs(t, err, commerceapp.ErrRefundAdminRequired)
	_, err = commerceapp.SyncAdminRefund(2, 3, "unknown")
	require.ErrorIs(t, err, commerceapp.ErrRefundAdminRequired)
	_, err = commerceapp.CreateAdminRefund(1, 2, request)
	require.Error(t, err)
	var order commerceschema.TopUp
	require.NoError(t, db.First(&order, "trade_no = ?", "other-owner").Error)
	require.Empty(t, order.RefundStatus, "another user's order cannot be refunded through a different target")
	// A cached caller role must not survive revocation.
	require.NoError(t, db.Model(&identityschema.User{}).Where("id = ?", 1).Update("role", constant.RoleCommonUser).Error)
	_, err = commerceapp.CreateAdminRefund(1, 3, request)
	require.ErrorIs(t, err, commerceapp.ErrRefundAdminRequired)
}

func TestAdminRefundHandlerRejectsInvalidTargetAndDirectNonAdmin(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	require.NoError(t, db.Create(&identityschema.User{Id: 2, Username: "direct-refund-user", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled}).Error)
	ctx, recorder := newCommerceContext(t, http.MethodGet, "/api/wallet/admin/users/0/refunds/eligible", nil, 2)
	ctx.Params = gin.Params{{Key: "id", Value: "0"}}
	listAdminRefundableOrders(ctx)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	ctx, recorder = newCommerceContext(t, http.MethodGet, "/api/wallet/admin/users/2/refunds/eligible", nil, 2)
	ctx.Params = gin.Params{{Key: "id", Value: "2"}}
	listAdminRefundableOrders(ctx)
	require.Equal(t, http.StatusForbidden, recorder.Code)
}
