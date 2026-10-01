package catalogcontrol

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRegisterWithoutAuthorizationRejectsEveryRoute(t *testing.T) {
	mux := http.NewServeMux()
	New(nil, nil, nil).Register(mux, nil)
	for _, endpoint := range []struct{ method, path string }{{"GET", "/api/catalog/channels"}, {"GET", "/api/settings"}, {"PUT", "/api/settings/foo"}, {"POST", "/api/catalog/channels/1/credentials"}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(`{}`))
		mux.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("%s %s: status %d", endpoint.method, endpoint.path, w.Code)
		}
	}
}

func TestWriteValidationPrecedesDatabaseAccess(t *testing.T) {
	mux := http.NewServeMux()
	New(nil, nil, nil).Register(mux, func(h http.Handler) http.Handler { return h })
	for _, test := range []struct{ path, body string }{{"/api/catalog/channels", `{"name":"ok","provider":"openai","base_url":"javascript:alert(1)"}`}, {"/api/catalog/channels", `{"name":"ok","provider":"openai","max_concurrency":-1}`}, {"/api/catalog/channels", `{"name":"ok","provider":"openai","settings":null}`}, {"/api/catalog/channels", `{"name":"ok","provider":"openai","unknown":true}`}, {"/api/catalog/channels", `{} {}`}} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", test.path, strings.NewReader(test.body)))
		if w.Code != 400 {
			t.Fatalf("%s: status %d: %s", test.body, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("PUT", "/api/settings/foo", strings.NewReader(`{"value":"secret"}`))
	r.Header.Set("Origin", "https://evil.test")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin status %d", w.Code)
	}
}

func TestSensitivityRecognizesLegacyOptionNames(t *testing.T) {
	for _, key := range []string{"StripeApiKey", "OIDCClientSecret", "SMTPPassword", "WeChatAccountToken", "payment.private_key"} {
		if !sensitiveKey(key) {
			t.Fatalf("secret option %s was public", key)
		}
	}
	if sensitiveKey("SystemName") {
		t.Fatal("public system name classified as secret")
	}
}

func TestOAuthExpiryImportFormats(t *testing.T) {
	for _, secret := range []string{`{"expired":"2030-01-01T00:00:00Z"}`, `{"expires_at":1893456000}`, `{"expiry_date":1893456000000}`} {
		got := oauthExpiry(secret)
		if got == nil || !got.Equal(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("%s: %v", secret, got)
		}
	}
	for _, secret := range []string{`plain-key`, `{"expires_at":"bad"}`, `{"expires_at":-1}`} {
		if oauthExpiry(secret) != nil {
			t.Fatalf("invalid expiry accepted: %s", secret)
		}
	}
}

func TestCredentialFingerprintValidationPrecedesStorage(t *testing.T) {
	mux := http.NewServeMux()
	New(nil, nil, nil).Register(mux, func(h http.Handler) http.Handler { return h })
	for _, body := range []string{`{"secret":"key","fingerprint":{"user_agent":"ua\r\nInjected: x"}}`, `{"secret":"key","fingerprint":{"tls_profile":"invalid"}}`} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/catalog/channels/1/credentials", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("invalid fingerprint status %d", w.Code)
		}
	}
}
