//go:build pgintegration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/migrations"
	"github.com/sh2001sh/new-api/v3/pkg/pg"
)

// This verifies the actual generated routing and session layer with a real
// disposable database. These inbox/market operations do not depend on Redis.
func TestMarketRatingAndNotificationAssembly(t *testing.T) {
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	pool, err := pg.Connect(ctx, pg.Config{DSN: dsn, MaxConns: 8})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	rows, err := pool.Query(ctx, `SELECT nspname FROM pg_namespace WHERE left(nspname,3)='v3_'`)
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range schemas {
		if _, err = pool.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	files, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		sql, e := migrations.Read(file)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, sql); e != nil {
			t.Fatalf("%s %v", file, e)
		}
	}
	key := bytes.Repeat([]byte{3}, 32)
	crypto, err := catalog.NewAESGCM(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{Identity: identity.ControlConfig{PublicURL: "http://control.test", SessionSecret: bytes.Repeat([]byte{5}, 32), EncryptionKey: key}}
	clearComposedPaymentEnvironment(t)
	h, err := controlHandler(&boot.Deps{PG: pool, Crypto: crypto}, cfg, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, token, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://control.test"+path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r.Header.Set("Origin", "http://control.test")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d want %d %s", method, path, w.Code, status, w.Body)
		}
		return w
	}
	w := call("POST", "/api/user/register", "", `{"username":"market_assembly","password":"strong-password","accepted_terms_version":"2026-10-07","accepted_privacy_version":"2026-10-07","agreement_locale":"en"}`, 200)
	var registered struct {
		Data identity.Session `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &registered); err != nil {
		t.Fatal(err)
	}
	token := registered.Data.AccessToken
	verifyCommunityFacade(t, pool.Pool, call, token)
	call("GET", "/api/marketplace/shops", "", "", 200)
	call("GET", "/api/marketplace/admin/shops", token, "", 403)
	call("GET", "/api/notifications", "", "", 401)
	call("GET", "/api/notifications", token, "", 200)
	call("GET", "/api/notifications/summary", token, "", 200)
	call("POST", "/api/notifications/read-all", token, `{}`, 400)
	call("POST", "/api/notifications/99999/read", token, `{"read":false}`, 404)
	r := httptest.NewRequest("POST", "http://control.test/api/notifications/read-all", strings.NewReader(`{"through_id":"1"}`))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Origin", "https://external.invalid")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-site write: %d %s", w.Code, w.Body)
	}
}
