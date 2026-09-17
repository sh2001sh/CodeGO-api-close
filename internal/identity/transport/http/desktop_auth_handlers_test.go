package http

import (
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestStartDesktopAuthSessionReturnsVerificationPayload(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	platformconfig.ServerAddress = "https://codegoai.com"
	t.Cleanup(func() {
		platformconfig.ServerAddress = "http://localhost:3000"
	})

	body := map[string]any{
		"device_name": "QA Laptop",
		"platform":    "windows",
		"app_version": "0.1.0",
	}
	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/auth/session", body, 0)
	StartDesktopAuthSession(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected desktop auth session create success, got %s", response.Message)
	}

	var payload identityapp.DesktopAuthStartResponse
	if err := platformencoding.Unmarshal(response.Data, &payload); err != nil {
		t.Fatalf("failed to decode desktop auth start payload: %v", err)
	}
	if payload.SessionID == "" || payload.UserCode == "" {
		t.Fatalf("expected session id and user code, got %#v", payload)
	}
	if !strings.Contains(payload.VerificationURI, "/desktop/authorize?") {
		t.Fatalf("expected verification uri, got %q", payload.VerificationURI)
	}
	if payload.Interval != identityapp.DesktopAuthPollInterval() {
		t.Fatalf("expected poll interval %d, got %d", identityapp.DesktopAuthPollInterval(), payload.Interval)
	}

	var stored identitydomain.DesktopAuthSession
	if err := db.First(&stored, "session_id = ?", payload.SessionID).Error; err != nil {
		t.Fatalf("failed to reload created auth session: %v", err)
	}
}

func TestGetDesktopAuthSessionReturnsAuthorizePageMetadata(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	user := &identityschema.User{Id: 1, Username: "desktop-authorize-user", Password: "password123", DisplayName: "Desktop Authorize User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	session := &identitydomain.DesktopAuthSession{
		SessionID:  "desktop-session-view",
		UserCode:   "ZXCV1122",
		DeviceName: "Review MacBook",
		Platform:   "macos",
		AppVersion: "2.0.0",
		Status:     identitydomain.DesktopAuthSessionStatusPending,
		CreatedAt:  platformruntime.GetTimestamp(),
		ExpiresAt:  platformruntime.GetTimestamp() + 600,
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("failed to seed desktop auth session: %v", err)
	}

	target := "/api/desktop/auth/session?session_id=" + url.QueryEscape(session.SessionID) + "&code=" + url.QueryEscape(session.UserCode)
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, target, nil, 1)
	ctx.Request.URL.RawQuery = "session_id=" + url.QueryEscape(session.SessionID) + "&code=" + url.QueryEscape(session.UserCode)
	GetDesktopAuthSession(ctx)
	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected desktop auth session details success, got %s", response.Message)
	}
}

func TestApproveDesktopAuthSessionAndPollReturnsDesktopAccessToken(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	platformconfig.ServerAddress = "https://codegoai.com"
	t.Cleanup(func() {
		platformconfig.ServerAddress = "http://localhost:3000"
	})

	user := &identityschema.User{Id: 1, Username: "desktop-auth-user", Password: "password123", DisplayName: "Desktop Auth User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	session := &identitydomain.DesktopAuthSession{
		SessionID:  "desktop-session-1",
		UserCode:   "ABCD1234",
		DeviceName: "Office Mac",
		Platform:   "macos",
		AppVersion: "1.2.3",
		Status:     identitydomain.DesktopAuthSessionStatusPending,
		CreatedAt:  platformruntime.GetTimestamp(),
		ExpiresAt:  platformruntime.GetTimestamp() + 600,
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("failed to seed desktop auth session: %v", err)
	}

	approveCtx, approveRecorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/auth/approve", map[string]any{"session_id": session.SessionID}, 1)
	ApproveDesktopAuthSession(approveCtx)
	approveResponse := decodeAPIResponse(t, approveRecorder)
	if !approveResponse.Success {
		t.Fatalf("expected approve desktop auth success, got %s", approveResponse.Message)
	}

	var approvePayload struct {
		DeviceID    int      `json:"device_id"`
		AccessToken string   `json:"access_token"`
		Scopes      []string `json:"scopes"`
		Status      string   `json:"status"`
	}
	if err := platformencoding.Unmarshal(approveResponse.Data, &approvePayload); err != nil {
		t.Fatalf("failed to decode approve payload: %v", err)
	}

	pollCtx, pollRecorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/auth/poll", map[string]any{"session_id": session.SessionID}, 0)
	PollDesktopAuthSession(pollCtx)
	pollResponse := decodeAPIResponse(t, pollRecorder)
	if !pollResponse.Success {
		t.Fatalf("expected poll success, got %s", pollResponse.Message)
	}

	var pollPayload identityapp.DesktopAuthPollResponse
	if err := platformencoding.Unmarshal(pollResponse.Data, &pollPayload); err != nil {
		t.Fatalf("failed to decode poll payload: %v", err)
	}
	if !pollPayload.Authenticated || pollPayload.AccessToken != approvePayload.AccessToken {
		t.Fatalf("unexpected poll payload: %#v", pollPayload)
	}
}

func TestRejectDesktopAuthSessionMarksSessionRejected(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	user := &identityschema.User{Id: 1, Username: "desktop-reject-user", Password: "password123", DisplayName: "Desktop Reject User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	session := &identitydomain.DesktopAuthSession{
		SessionID:  "desktop-session-reject",
		UserCode:   "WXYZ5678",
		DeviceName: "QA Desktop",
		Platform:   "windows",
		AppVersion: "1.0.0",
		Status:     identitydomain.DesktopAuthSessionStatusPending,
		CreatedAt:  platformruntime.GetTimestamp(),
		ExpiresAt:  platformruntime.GetTimestamp() + 600,
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("failed to seed desktop auth session: %v", err)
	}

	rejectCtx, rejectRecorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/auth/reject", map[string]any{"session_id": session.SessionID}, 1)
	RejectDesktopAuthSession(rejectCtx)
	rejectResponse := decodeAPIResponse(t, rejectRecorder)
	if !rejectResponse.Success {
		t.Fatalf("expected reject desktop auth success, got %s", rejectResponse.Message)
	}
}

func TestPollDesktopAuthSessionMarksExpiredPendingSession(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	session := &identitydomain.DesktopAuthSession{
		SessionID:  "desktop-session-expired",
		UserCode:   "EXPD0001",
		DeviceName: "Old Desktop",
		Platform:   "windows",
		AppVersion: "0.8.0",
		Status:     identitydomain.DesktopAuthSessionStatusPending,
		CreatedAt:  platformruntime.GetTimestamp() - 1200,
		ExpiresAt:  platformruntime.GetTimestamp() - 60,
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("failed to seed desktop auth session: %v", err)
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/auth/poll", map[string]any{"session_id": session.SessionID}, 0)
	PollDesktopAuthSession(ctx)
	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected poll success for expired session, got %s", response.Message)
	}
}

func TestListAndRevokeDesktopAuthorizedDevice(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	user := &identityschema.User{Id: 1, Username: "desktop-device-owner", Password: "password123", DisplayName: "Desktop Device Owner", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	device := &identitydomain.DesktopAuthorizedDevice{
		UserID:      user.Id,
		DeviceName:  "ThinkPad",
		Platform:    "windows",
		AppVersion:  "0.9.0",
		AccessToken: "desktop_test_token",
		Scopes:      identityapp.SerializeDesktopScopes([]string{identitydomain.DesktopScopeAccountRead, identitydomain.DesktopScopeConfigRead}),
		Status:      identitydomain.DesktopAuthorizedDeviceStatusActive,
		CreatedAt:   platformruntime.GetTimestamp(),
		LastUsedAt:  platformruntime.GetTimestamp(),
		ExpiresAt:   platformruntime.GetTimestamp() + 3600,
	}
	if err := db.Create(device).Error; err != nil {
		t.Fatalf("failed to seed desktop device: %v", err)
	}

	listCtx, listRecorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/devices", nil, 1)
	ListDesktopAuthorizedDevices(listCtx)
	listResponse := decodeAPIResponse(t, listRecorder)
	if !listResponse.Success {
		t.Fatalf("expected list devices success, got %s", listResponse.Message)
	}

	revokeCtx, revokeRecorder := newAuthenticatedContext(t, http.MethodDelete, fmt.Sprintf("/api/desktop/devices/%d", device.Id), nil, 1)
	revokeCtx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(device.Id)}}
	RevokeDesktopAuthorizedDevice(revokeCtx)
	revokeResponse := decodeAPIResponse(t, revokeRecorder)
	if !revokeResponse.Success {
		t.Fatalf("expected revoke device success, got %s", revokeResponse.Message)
	}
}

func TestDesktopRouteScopeEnforcementRejectsMissingScope(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	user := &identityschema.User{Id: 1, Username: "desktop-scope-user", Password: "password123", DisplayName: "Desktop Scope User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	device := &identitydomain.DesktopAuthorizedDevice{
		UserID:      user.Id,
		DeviceName:  "Scoped Device",
		Platform:    "windows",
		AppVersion:  "1.0.0",
		AccessToken: "desktop_scope_token",
		Scopes:      identityapp.SerializeDesktopScopes([]string{identitydomain.DesktopScopeAccountRead}),
		Status:      identitydomain.DesktopAuthorizedDeviceStatusActive,
		CreatedAt:   platformruntime.GetTimestamp(),
		LastUsedAt:  platformruntime.GetTimestamp(),
		ExpiresAt:   platformruntime.GetTimestamp() + 3600,
	}
	if err := db.Create(device).Error; err != nil {
		t.Fatalf("failed to seed desktop device: %v", err)
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/desktop/usage/logs", nil)
	req.Header.Set("Authorization", "Bearer "+device.AccessToken)
	req.Header.Set("New-Api-User", strconv.Itoa(user.Id))
	ctx.Request = req

	middleware.DesktopAuth()(ctx)
	if ctx.IsAborted() {
		t.Fatalf("expected desktop auth to pass before scope check")
	}
	middleware.RequireDesktopScope(identitydomain.DesktopScopeLogsRead)(ctx)
	if !ctx.IsAborted() {
		t.Fatalf("expected scope middleware to abort request")
	}
}
