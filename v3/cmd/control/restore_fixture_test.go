//go:build pgintegration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/migrations"
	"github.com/sh2001sh/new-api/v3/pkg/pg"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

type restoredStack struct {
	pool *pgxpool.Pool
	http http.Handler
	now  *time.Time
}

func restoreStack(t *testing.T, modify func(*config)) *restoredStack {
	t.Helper()
	dsn, addr := os.Getenv("V3_TEST_PG_DSN"), os.Getenv("V3_TEST_REDIS_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("V3_TEST_PG_DSN / V3_TEST_REDIS_ADDR not set")
	}
	clearComposedPaymentEnvironment(t)
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("restore_control_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	dbConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	dbConfig.ConnConfig.Database = name
	dbConfig.MaxConns = 5
	inner, err := pgxpool.NewWithConfig(ctx, dbConfig)
	if err != nil {
		t.Fatal(err)
	}
	pool := &pg.Pool{Pool: inner}
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Errorf("remove isolated restore database: %v", err)
		}
		_ = admin.Close(ctx)
	})
	files, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		sql, err := migrations.Read(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("migration %s: %v", file, err)
		}
	}
	// Separate databases share test Redis. Account and posting marker IDs must
	// not collide with balances from an earlier isolated fixture.
	sequence := time.Now().UnixNano()%1_000_000_000 + 1_000_000_000
	for _, name := range []string{"v3_billing.accounts_id_seq", "v3_billing.balance_outbox_id_seq"} {
		if _, err := pool.Exec(ctx, `SELECT setval($1::regclass,$2)`, name, sequence); err != nil {
			t.Fatal(err)
		}
	}
	rdb, err := redisx.Connect(redisx.Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	key := bytes.Repeat([]byte{3}, 32)
	crypto, err := catalog.NewAESGCM(key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)
	cfg := config{Identity: identity.ControlConfig{PublicURL: "http://control.test", SessionSecret: bytes.Repeat([]byte{5}, 32), EncryptionKey: key, Now: func() time.Time { return now }}}
	if modify != nil {
		modify(&cfg)
	}
	h, err := controlHandler(&boot.Deps{PG: pool, Redis: rdb, Crypto: crypto}, cfg, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return &restoredStack{pool: pool.Pool, http: h, now: &now}
}

func (s *restoredStack) call(t *testing.T, method, path, token, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://control.test"+path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CodeGo-API-Version", "3")
	r.Header.Set("Origin", "http://control.test")
	w := httptest.NewRecorder()
	s.http.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, w.Code, status, w.Body)
	}
	return w
}

func restoreData[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var response struct {
		Success bool `json:"success"`
		Data    T    `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || !response.Success {
		t.Fatalf("restore response: success=%v err=%v body=%s", response.Success, err, w.Body)
	}
	return response.Data
}

func (s *restoredStack) register(t *testing.T, username string) identity.Session {
	t.Helper()
	w := s.call(t, "POST", "/api/user/register", "", fmt.Sprintf(`{"username":%q,"password":"strong-password","accepted_terms_version":"2026-10-07","accepted_privacy_version":"2026-10-07","agreement_locale":"en"}`, username), 200)
	return restoreData[identity.Session](t, w)
}
