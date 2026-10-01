//go:build pgintegration

package oidc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func testClientAndRevocation(t *testing.T, f *fixture) {
	t.Helper()
	code := f.code(t, 1, "openid")
	var stored []byte
	if err := f.pool.QueryRow(context.Background(), `SELECT code_hash FROM v3_identity.oidc_codes WHERE code_hash=$1`, digest(code)).Scan(&stored); err != nil || string(stored) == code {
		t.Fatalf("authorization code not hashed: %v", err)
	}
	for _, credentials := range [][2]string{{"wrong-client", f.s.cfg.ClientSecret}, {f.s.cfg.ClientID, "wrong-secret"}} {
		r := httptest.NewRequest(http.MethodPost, "/api/oidc/token", strings.NewReader(tokenValues(f.s.cfg, code).Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.SetBasicAuth(credentials[0], credentials[1])
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != 401 || !strings.Contains(w.Body.String(), "invalid_client") || w.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("client mismatch accepted: %d %s", w.Code, w.Body.String())
		}
	}
	result := decodeResult(t, f.exchange(tokenValues(f.s.cfg, code), true))
	var token map[string]any
	parts := strings.Split(result.IDToken, ".")
	if len(parts) != 3 {
		t.Fatal("malformed ID token")
	}
	// The signature is checked in the main flow; decode here to inspect scope.
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &token); err != nil {
		t.Fatal(err)
	}
	if token["email"] != nil || token["name"] != nil || token["preferred_username"] != nil {
		t.Fatalf("ID token scope leak: %+v", token)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE v3_identity.oidc_tokens SET revoked_at=now() WHERE token_hash=$1`, digest(result.AccessToken)); err != nil {
		t.Fatal(err)
	}
	if w := f.info(result.AccessToken, http.MethodGet); w.Code != 401 {
		t.Fatalf("revoked token accepted: %d", w.Code)
	}
}

func testConcurrentSubject(t *testing.T, f *fixture) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO v3_identity.users(id,username) VALUES (3,'oidc_concurrent')`); err != nil {
		t.Fatal(err)
	}
	codes := make([]string, 6)
	for i := range codes {
		codes[i] = f.code(t, 3, "openid")
	}
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, len(codes))
	for _, code := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- f.exchange(tokenValues(f.s.cfg, code), true)
		}()
	}
	wg.Wait()
	close(results)
	var subject string
	for w := range results {
		result := decodeResult(t, w)
		claims := decodeClaims(t, f.info(result.AccessToken, http.MethodGet))
		sub, ok := claims["sub"].(string)
		if !ok || len(sub) != 6 || subject != "" && subject != sub {
			t.Fatalf("unstable subject: previous=%s new=%v", subject, claims)
		}
		subject = sub
	}
}
