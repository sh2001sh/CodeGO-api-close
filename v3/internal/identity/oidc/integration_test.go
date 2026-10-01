//go:build pgintegration

package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/migrations"
)

type fixture struct {
	s     *Server
	pool  *pgxpool.Pool
	clock atomic.Int64
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// The integration DSN must refer to the disposable cluster used by verify.sh.
	// Packages run serially there and each suite resets its test schemas.
	for _, schema := range []string{"v3_community", "v3_audit", "v3_marketplace", "v3_commerce", "v3_billing", "v3_catalog", "v3_identity", "v3_platform"} {
		if _, err = pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	files := []string{"20261001000001_platform.sql", "20261001000002_identity.sql", "20261001000019_oidc.sql"}
	for _, name := range files {
		sql, readErr := migrations.Read(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err = pool.Exec(ctx, sql); err != nil {
			t.Fatalf("migration %s: %v", name, err)
		}
	}
	f := &fixture{pool: pool}
	f.clock.Store(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Unix())
	c := testConfig(t)
	c.Now = func() time.Time { return time.Unix(f.clock.Load(), 0) }
	f.s, err = New(pool, c, func(r *http.Request) (int64, error) {
		uid, e := strconv.ParseInt(r.Header.Get("X-Test-User"), 10, 64)
		if e != nil || uid <= 0 {
			return 0, errors.New("no session")
		}
		return uid, nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO v3_identity.users(id,external_id,username,display_name,email,email_verified)
	 VALUES (1,'ABC234','oidc_alice','Alice','alice@example.test',true),
	 (2,NULL,'oidc_bob','','bob@example.test',false)`)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) code(t *testing.T, uid int64, scope string) string {
	t.Helper()
	q := authorizationValues(f.s.cfg)
	q.Set("scope", scope)
	r := httptest.NewRequest(http.MethodGet, "/api/oidc/authorize?"+q.Encode(), nil)
	r.Header.Set("X-Test-User", strconv.FormatInt(uid, 10))
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil || w.Code != 302 || u.Query().Get("code") == "" || u.Query().Get("state") != "browser-state" {
		t.Fatalf("authorization failed: %d %q %s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	return u.Query().Get("code")
}

func tokenValues(cfg Config, code string) url.Values {
	return url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {cfg.RedirectURI}, "code_verifier": {strings.Repeat("v", 43)}}
}

func (f *fixture) exchange(form url.Values, basic bool) *httptest.ResponseRecorder {
	if !basic {
		form.Set("client_id", f.s.cfg.ClientID)
		form.Set("client_secret", f.s.cfg.ClientSecret)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/oidc/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basic {
		r.SetBasicAuth(f.s.cfg.ClientID, f.s.cfg.ClientSecret)
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func (f *fixture) info(token string, method string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/oidc/userinfo", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func decodeResult(t *testing.T, w *httptest.ResponseRecorder) tokenResult {
	t.Helper()
	var result tokenResult
	if w.Code != 200 {
		t.Fatalf("token rejected: %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func decodeClaims(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var claims map[string]any
	if w.Code != 200 {
		t.Fatalf("userinfo rejected: %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

func TestOIDCPostgresFlow(t *testing.T) {
	f := setup(t)
	t.Run("signature scopes and persisted subject", func(t *testing.T) {
		code := f.code(t, 1, "openid profile email")
		result := decodeResult(t, f.exchange(tokenValues(f.s.cfg, code), true))
		verifyIDTokenAndJWK(t, f.s, result.IDToken, "ABC234")
		claims := decodeClaims(t, f.info(result.AccessToken, http.MethodGet))
		if claims["sub"] != "ABC234" || claims["email_verified"] != true || claims["name"] != "Alice" {
			t.Fatalf("claims=%+v", claims)
		}
		w := f.exchange(tokenValues(f.s.cfg, code), true)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_grant") {
			t.Fatalf("code replay accepted: %d %s", w.Code, w.Body.String())
		}
		var count int
		if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM v3_identity.oidc_tokens WHERE token_hash=$1`, digest(result.AccessToken)).Scan(&count); err != nil || count != 1 {
			t.Fatalf("token digest missing: %d %v", count, err)
		}
	})
	t.Run("wrong verifier and redirect do not consume", func(t *testing.T) {
		code := f.code(t, 1, "openid")
		for _, mutate := range []func(url.Values){
			func(v url.Values) { v.Set("code_verifier", strings.Repeat("w", 43)) },
			func(v url.Values) { v.Set("redirect_uri", "https://evil.test") },
		} {
			v := tokenValues(f.s.cfg, code)
			mutate(v)
			w := f.exchange(v, true)
			if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_grant") {
				t.Fatalf("invalid grant accepted: %d %s", w.Code, w.Body.String())
			}
		}
		result := decodeResult(t, f.exchange(tokenValues(f.s.cfg, code), false))
		if claims := decodeClaims(t, f.info(result.AccessToken, http.MethodPost)); len(claims) != 1 {
			t.Fatalf("scope leaked: %+v", claims)
		}
	})
	t.Run("expiry and disabled users", func(t *testing.T) {
		code := f.code(t, 1, "openid")
		f.clock.Add(int64(codeTTL.Seconds()))
		w := f.exchange(tokenValues(f.s.cfg, code), true)
		if w.Code != 400 {
			t.Fatalf("expired code accepted: %d", w.Code)
		}
		code = f.code(t, 1, "openid")
		result := decodeResult(t, f.exchange(tokenValues(f.s.cfg, code), true))
		code = f.code(t, 1, "openid")
		if _, err := f.pool.Exec(context.Background(), `UPDATE v3_identity.users SET status='disabled' WHERE id=1`); err != nil {
			t.Fatal(err)
		}
		if w = f.exchange(tokenValues(f.s.cfg, code), true); w.Code != 400 {
			t.Fatalf("disabled user grant accepted: %d", w.Code)
		}
		if w = f.info(result.AccessToken, http.MethodGet); w.Code != 401 {
			t.Fatalf("disabled user token accepted: %d", w.Code)
		}
		if _, err := f.pool.Exec(context.Background(), `UPDATE v3_identity.users SET status='active' WHERE id=1`); err != nil {
			t.Fatal(err)
		}
		f.clock.Add(int64(tokenTTL.Seconds()))
		if w = f.info(result.AccessToken, http.MethodGet); w.Code != 401 {
			t.Fatalf("expired token accepted: %d", w.Code)
		}
	})
	t.Run("missing subject and unverified email", func(t *testing.T) {
		code := f.code(t, 2, "openid email")
		result := decodeResult(t, f.exchange(tokenValues(f.s.cfg, code), true))
		claims := decodeClaims(t, f.info(result.AccessToken, http.MethodGet))
		sub, ok := claims["sub"].(string)
		if !ok || len(sub) != 6 || claims["email_verified"] != false {
			t.Fatalf("unsafe claims=%+v", claims)
		}
		code = f.code(t, 2, "openid profile")
		result = decodeResult(t, f.exchange(tokenValues(f.s.cfg, code), true))
		claims = decodeClaims(t, f.info(result.AccessToken, http.MethodGet))
		if claims["sub"] != sub || claims["email"] != nil || claims["name"] != "oidc_bob" {
			t.Fatalf("subject/profile=%+v", claims)
		}
	})
	t.Run("concurrent consume one winner", func(t *testing.T) { testConcurrentExchange(t, f) })
	t.Run("client authentication and revocation", func(t *testing.T) { testClientAndRevocation(t, f) })
	t.Run("concurrent initialization stable subject", func(t *testing.T) { testConcurrentSubject(t, f) })
	t.Run("self editable settings cannot verify email", func(t *testing.T) {
		if _, err := f.pool.Exec(context.Background(), `UPDATE v3_identity.users SET settings='{"email_verified":true}' WHERE id=2`); err != nil {
			t.Fatal(err)
		}
		result := decodeResult(t, f.exchange(tokenValues(f.s.cfg, f.code(t, 2, "openid email")), true))
		claims := decodeClaims(t, f.info(result.AccessToken, http.MethodGet))
		if claims["email_verified"] != false {
			t.Fatalf("self-edited settings forged verification: %+v", claims)
		}
	})
	t.Run("community subject helper with OIDC disabled", func(t *testing.T) { testEnsureSubject(t, f) })
}
