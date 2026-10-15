package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
)

type turnstileRoundTripper func(*http.Request) (*http.Response, error)

func (f turnstileRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRegistrationTurnstileCheckDoesNotReuseSessionVerification(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousEnabled := platformconfig.TurnstileCheckEnabled
	platformconfig.TurnstileCheckEnabled = true
	t.Cleanup(func() { platformconfig.TurnstileCheckEnabled = previousEnabled })

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("registration-turnstile-test"))))
	engine.GET("/seed", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("turnstile", true)
		if err := session.Save(); err != nil {
			t.Fatalf("failed to save session: %v", err)
		}
		c.Status(http.StatusNoContent)
	})
	engine.POST("/register", RegistrationTurnstileCheck(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	seedRecorder := httptest.NewRecorder()
	engine.ServeHTTP(seedRecorder, httptest.NewRequest(http.MethodGet, "/seed", nil))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/register", nil)
	request.Header.Set("Cookie", seedRecorder.Header().Get("Set-Cookie"))
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected fresh token rejection, got status %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Turnstile token") {
		t.Fatalf("expected missing token response, got %s", recorder.Body.String())
	}
}

func TestRegistrationTurnstileCheckAcceptsFreshToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousEnabled := platformconfig.TurnstileCheckEnabled
	previousClient := turnstileHTTPClient
	platformconfig.TurnstileCheckEnabled = true
	turnstileHTTPClient = &http.Client{Transport: turnstileRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", req.Method)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		if !strings.Contains(string(body), "response=fresh-token") {
			t.Fatalf("expected turnstile token in request, got %s", body)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"success":true}`)),
			Header:     make(http.Header),
		}, nil
	})}
	t.Cleanup(func() {
		platformconfig.TurnstileCheckEnabled = previousEnabled
		turnstileHTTPClient = previousClient
	})

	engine := gin.New()
	engine.POST("/register", RegistrationTurnstileCheck(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/register?turnstile=fresh-token", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected registration handler to run, got status %d", recorder.Code)
	}
}

func TestRateLimitKeyExpirationCoversRequestedWindow(t *testing.T) {
	previousExpiration := platformconfig.RateLimitKeyExpirationDuration
	platformconfig.RateLimitKeyExpirationDuration = 20 * time.Minute
	t.Cleanup(func() { platformconfig.RateLimitKeyExpirationDuration = previousExpiration })

	if got := rateLimitKeyExpiration(24 * 60 * 60); got != 24*time.Hour {
		t.Fatalf("expected daily rate limit key to last 24h, got %s", got)
	}
	if got := rateLimitKeyExpiration(60); got != 20*time.Minute {
		t.Fatalf("expected short window to retain default expiration, got %s", got)
	}
}
