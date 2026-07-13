package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func TestBuildSessionStoreUsesLaxSameSite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", buildSessionStore()))
	router.GET("/", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("oauth_state", "state")
		if err := session.Save(); err != nil {
			t.Fatalf("save session: %v", err)
		}
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	cookie, err := http.ParseSetCookie(recorder.Header().Get("Set-Cookie"))
	if err != nil {
		t.Fatalf("parse session cookie: %v", err)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want %v", cookie.SameSite, http.SameSiteLaxMode)
	}
}
