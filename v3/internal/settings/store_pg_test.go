//go:build pgintegration

package settings

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/migrations"
)

var ctx = context.Background()

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	rows, err := pool.Query(ctx, `SELECT nspname FROM pg_namespace WHERE left(nspname, 3) = 'v3_'`)
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range schemas {
		if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{s}.Sanitize()+" CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	names, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		sql, err := migrations.Read(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("apply %s: %v", n, err)
		}
	}
	return pool
}

func aead(t *testing.T) *catalog.AESGCM {
	t.Helper()
	a, err := catalog.NewAESGCM(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestGetPlainAndSensitiveSettings(t *testing.T) {
	pool := testPool(t)
	crypto := aead(t)
	secret, err := crypto.Encrypt([]byte(`{"api_key":"sk-upstream"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_platform.settings (key, value) VALUES ('site.name', '"CodeGo"')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_platform.settings (key, ciphertext, sensitive) VALUES ('pay.stripe', $1, true)`, secret); err != nil {
		t.Fatal(err)
	}
	s := New(pool, crypto)

	got, err := s.Get(ctx, "site.name")
	if err != nil || string(got) != `"CodeGo"` {
		t.Fatalf("plain setting = %s, %v", got, err)
	}
	got, err = s.Get(ctx, "pay.stripe")
	if err != nil || string(got) != `{"api_key":"sk-upstream"}` {
		t.Fatalf("sensitive setting = %s, %v", got, err)
	}
}

// A sensitive value must never come back as ciphertext or plaintext when the
// store cannot decrypt it, and a missing key is an error, not an empty value.
func TestGetFailsClosed(t *testing.T) {
	pool := testPool(t)
	crypto := aead(t)
	secret, _ := crypto.Encrypt([]byte(`"hidden"`))
	if _, err := pool.Exec(ctx, `INSERT INTO v3_platform.settings (key, ciphertext, sensitive) VALUES ('s', $1, true)`, secret); err != nil {
		t.Fatal(err)
	}
	notJSON, _ := crypto.Encrypt([]byte(`not json`))
	if _, err := pool.Exec(ctx, `INSERT INTO v3_platform.settings (key, ciphertext, sensitive) VALUES ('bad', $1, true)`, notJSON); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		store *Store
		key   string
		want  string
	}{
		{"no decrypter", New(pool, nil), "s", "decryption is unavailable"},
		{"wrong key", New(pool, func() *catalog.AESGCM {
			other, _ := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
			return other
		}()), "s", "decrypt"},
		{"decrypts to invalid JSON", New(pool, crypto), "bad", "invalid JSON"},
		{"missing", New(pool, crypto), "nope", "read"},
	}
	for _, c := range cases {
		got, err := c.store.Get(ctx, c.key)
		if err == nil || !strings.Contains(err.Error(), c.want) || got != nil {
			t.Errorf("%s: Get = %q, %v; want nil and error containing %q", c.name, got, err, c.want)
		}
	}
	if _, err := New(pool, crypto).Get(ctx, "nope"); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("missing key error %v does not wrap pgx.ErrNoRows", err)
	}
}
