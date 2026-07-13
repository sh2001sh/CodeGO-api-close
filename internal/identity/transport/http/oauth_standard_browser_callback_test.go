package http

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/internal/identity/oauth"
)

func TestHandleOAuthRedirectsBrowserCallbackToFrontend(t *testing.T) {
	setupDesktopHTTPTestDB(t)

	provider := &stubOAuthProvider{
		name:           "StubOAuth",
		enabled:        true,
		providerUserID: "browser-provider-id",
		username:       "browser-oauth-user",
		displayName:    "Browser OAuth User",
		email:          "browser-oauth@example.com",
	}
	oauth.Register("stub-browser", provider)
	t.Cleanup(func() {
		oauth.Unregister("stub-browser")
	})

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("oauth-test-secret"))))
	engine.GET("/prepare", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("oauth_state", "browser-state")
		if err := session.Save(); err != nil {
			t.Fatalf("save oauth state: %v", err)
		}
		c.Status(http.StatusNoContent)
	})
	engine.GET("/oauth/:provider", HandleOAuth)

	prepareRecorder := httptest.NewRecorder()
	engine.ServeHTTP(prepareRecorder, httptest.NewRequest(http.MethodGet, "/prepare", nil))

	req := httptest.NewRequest(http.MethodGet, "/oauth/stub-browser?state=browser-state&code=oauth-code", nil)
	req.Host = "shu26.cfd"
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Cookie", prepareRecorder.Header().Get("Set-Cookie"))
	req.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusFound)
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if got, want := location.String(), "https://shu26.cfd/oauth/stub-browser?authenticated=1&uid=1"; got != want {
		t.Fatalf("redirect location = %q, want %q", got, want)
	}
}
