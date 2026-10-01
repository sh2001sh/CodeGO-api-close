package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOAuthRotationPreservesAccountMetadataAndMissingRefreshToken(t *testing.T) {
	var gotUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.UserAgent()
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("refresh_token") != "old-refresh" || r.Form.Get("client_id") != "client" {
			t.Error("incorrect OAuth refresh form")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"new-access","expires_in":3600}`)
	}))
	defer server.Close()
	transports := NewTransportPool(TransportConfig{})
	defer transports.CloseIdle()
	r, err := NewOAuthRefresher(OAuthConfig{TokenURL: server.URL, ClientID: "client"}, transports)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := r.Refresh(context.Background(), Credential{ID: 1, Secret: []byte(`{"access_token":"old-access","refresh_token":"old-refresh","account_id":"account","unknown":{"keep":true}}`), Fingerprint: Fingerprint{UserAgent: "stable-ua", TLSProfile: "firefox"}})
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err = json.Unmarshal(fresh.Secret, &document); err != nil {
		t.Fatal(err)
	}
	if string(document["access_token"]) != `"new-access"` || string(document["refresh_token"]) != `"old-refresh"` || string(document["account_id"]) != `"account"` || string(document["unknown"]) != `{"keep":true}` {
		t.Fatalf("unexpected refreshed document: %s", fresh.Secret)
	}
	if gotUA != "stable-ua" || fresh.Fingerprint.TLSProfile != "firefox" {
		t.Fatal("fingerprint changed during refresh")
	}
	if until := time.Until(fresh.ExpiresAt); until < 59*time.Minute || until > time.Hour {
		t.Fatalf("wrong expiry: %v", until)
	}
}

func TestOAuthRejectsRedirectsMalformedAndSecretBearingFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"redirect", 302, "secret-in-error"}, {"rate_limit", 429, "sensitive-access-token"},
		{"missing_access_token", 200, `{"expires_in":60}`}, {"overflow_expiry", 200, `{"access_token":"a","expires_in":9223372036854775807}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			redirected := false
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
			defer target.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", target.URL)
				w.Header().Set("Retry-After", "13")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			transports := NewTransportPool(TransportConfig{})
			defer transports.CloseIdle()
			r, err := NewOAuthRefresher(OAuthConfig{TokenURL: server.URL, ClientID: "client"}, transports)
			if err != nil {
				t.Fatal(err)
			}
			_, err = r.Refresh(context.Background(), Credential{ID: 1, Secret: []byte(`{"refresh_token":"refresh-secret"}`)})
			if err == nil {
				t.Fatal("invalid endpoint response accepted")
			}
			if redirected || strings.Contains(err.Error(), tc.body) || strings.Contains(err.Error(), "refresh-secret") {
				t.Fatalf("unsafe failure: %v", err)
			}
			if tc.status == 429 {
				var retry *RetryError
				if !errors.As(err, &retry) || retry.After != 13*time.Second {
					t.Fatalf("lost Retry-After: %v", err)
				}
			}
		})
	}
}

func TestOAuthRejectsInsecureRemoteTokenEndpoint(t *testing.T) {
	_, err := NewOAuthRefresher(OAuthConfig{TokenURL: "http://example.com/token", ClientID: "client"}, NewTransportPool(TransportConfig{}))
	if err == nil {
		t.Fatal("insecure endpoint accepted")
	}
}

func TestDefaultRefreshersCoverMigratedProviderNames(t *testing.T) {
	pool := NewTransportPool(TransportConfig{})
	defer pool.CloseIdle()
	refreshers, err := DefaultRefreshers(pool, OAuthConfig{ClientID: "configured-gemini-client"})
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"codex", "anthropic", "gemini"} {
		if refreshers[provider] == nil {
			t.Errorf("missing refresher for migrated provider %q", provider)
		}
	}
}
