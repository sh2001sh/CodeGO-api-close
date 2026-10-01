//go:build pgintegration

package catalog

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sh2001sh/new-api/v3/migrations"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// Run with:
//
//	V3_TEST_PG_DSN=postgres://... V3_TEST_REDIS_ADDR=127.0.0.1:port \
//	  go test -tags=pgintegration -count=1 ./internal/catalog/...
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `DO $$ DECLARE s record; BEGIN
		FOR s IN SELECT nspname FROM pg_namespace WHERE left(nspname,3)='v3_' LOOP
			EXECUTE format('DROP SCHEMA %I CASCADE', s.nspname);
		END LOOP;
	END $$`); err != nil {
		t.Fatalf("reset disposable schemas: %v", err)
	}
	names, err := migrations.Files()
	if err != nil {
		t.Fatalf("migrations.Files: %v", err)
	}
	for _, name := range names {
		sql, err := migrations.Read(name)
		if err != nil {
			t.Fatalf("migrations.Read(%s): %v", name, err)
		}
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return pool
}

func testRedis(t *testing.T) *redisx.Client {
	t.Helper()
	addr := os.Getenv("V3_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("V3_TEST_REDIS_ADDR not set")
	}
	client, err := redisx.Connect(redisx.Config{Addr: addr})
	if err != nil {
		t.Fatalf("redis connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flushdb: %v", err)
	}
	return client
}

func testDecrypter(t *testing.T) (*AESGCM, *AESGCM) {
	t.Helper()
	a, err := NewAESGCM(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatalf("NewAESGCM: %v", err)
	}
	return a, a // one key serves as both Decrypter and Encrypter here
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %s: %v", sql, err)
	}
}

// seedCatalog inserts one group, two channels each with one credential, model
// membership and group membership, matching the fixtures the tests below
// assert on.
func seedCatalog(t *testing.T, pool *pgxpool.Pool, enc Encrypter) {
	t.Helper()
	mustExec(t, pool, `INSERT INTO v3_catalog.groups (name, multiplier) VALUES ('default', 1)`)
	mustExec(t, pool, `INSERT INTO v3_catalog.channels (id, name, provider, priority, weight)
		OVERRIDING SYSTEM VALUE VALUES (1, 'openai-a', 'openai', 10, 0)`)
	mustExec(t, pool, `INSERT INTO v3_catalog.channels (id, name, provider, priority, weight)
		OVERRIDING SYSTEM VALUE VALUES (2, 'openai-b', 'openai', 5, 3)`)
	mustExec(t, pool, `INSERT INTO v3_catalog.channel_groups (channel_id, group_name) VALUES (1, 'default'), (2, 'default')`)
	mustExec(t, pool, `INSERT INTO v3_catalog.channel_models (channel_id, model) VALUES (1, 'gpt-4'), (2, 'gpt-4')`)

	for _, ch := range []struct {
		id     int64
		secret string
	}{{1, "sk-alpha"}, {2, "sk-beta"}} {
		ct, err := enc.Encrypt([]byte(ch.secret))
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		mustExec(t, pool, `INSERT INTO v3_catalog.channel_credentials (channel_id, kind, secret) VALUES ($1, 'api_key', $2)`, ch.id, ct)
	}
}
