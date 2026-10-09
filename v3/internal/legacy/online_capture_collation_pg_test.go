//go:build pgintegration

package legacy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func onlineCaptureCollationPool(t *testing.T, locale string) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("V3_MIGRATION_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_MIGRATION_TEST_PG_DSN is required for an isolated fixture")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	random := make([]byte, 8)
	if _, err = rand.Read(random); err != nil {
		t.Fatal(err)
	}
	name := "capture_collation_" + hex.EncodeToString(random)
	query := "CREATE DATABASE " + pgx.Identifier{name}.Sanitize() + " TEMPLATE template0 LOCALE_PROVIDER libc LC_COLLATE " + onlineCaptureLiteral(locale) + " LC_CTYPE " + onlineCaptureLiteral(locale)
	if _, err = admin.Exec(ctx, query); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestOnlineCaptureDatabaseCollationDoesNotFabricateDDLDrift(t *testing.T) {
	for _, locale := range []string{"C", "en_US.UTF-8"} {
		t.Run(locale, func(t *testing.T) {
			pool := onlineCaptureCollationPool(t, locale)
			ctx := context.Background()
			onlineCaptureTestExec(t, pool, `CREATE SCHEMA marketplace;
 CREATE TABLE marketplace.channel_user_blocks(id bigint PRIMARY KEY,payload text);
 CREATE TABLE marketplace.channels(id bigint PRIMARY KEY,payload text);
 CREATE TABLE public.user_subscriptions(id bigint PRIMARY KEY,payload text);
 CREATE TABLE public.users(id bigint PRIMARY KEY,payload text)`)
			var catalogOrder, defaultOrder, byteOrder []string
			err := pool.QueryRow(ctx, `WITH names AS (
 SELECT n.nspname||'.'||c.relname AS name FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE c.relkind='r' AND n.nspname IN('marketplace','public'))
 SELECT array_agg(name ORDER BY name),array_agg(name ORDER BY name COLLATE "default"),
 array_agg(name ORDER BY name COLLATE "C") FROM names`).Scan(&catalogOrder, &defaultOrder, &byteOrder)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(catalogOrder, byteOrder) {
				t.Fatal("catalog name expression did not retain C collation")
			}
			if slices.Equal(catalogOrder, defaultOrder) != (locale == "C") {
				t.Fatalf("fixture did not exercise the legacy ordering boundary: locale=%s catalog=%v registry_default=%v", locale, catalogOrder, defaultOrder)
			}
			t.Logf("locale=%s catalog=%v registry_default=%v", locale, catalogOrder, defaultOrder)
			onlineCaptureTestInstall(t, pool)
			if err = onlineCaptureTestValidate(pool); err != nil {
				t.Fatalf("unchanged schema rejected with locale %s: %v", locale, err)
			}
			var stored []string
			if err = pool.QueryRow(ctx, `SELECT array_agg(name ORDER BY name COLLATE "C") FROM v3_migration_capture.tables`).Scan(&stored); err != nil || !slices.Equal(stored, catalogOrder) {
				t.Fatalf("capture registry ordering changed: stored=%v err=%v", stored, err)
			}
			onlineCaptureTestExec(t, pool, `INSERT INTO marketplace.channels VALUES(1,'still captured')`)
			var events int64
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_migration_capture.events WHERE table_name='marketplace.channels' AND row_key='{"id":1}'`).Scan(&events); err != nil || events != 1 {
				t.Fatalf("write capture changed: count=%d err=%v", events, err)
			}
			onlineCaptureTestExec(t, pool, `ALTER TABLE marketplace.channels ADD COLUMN real_ddl_drift text`)
			if err = onlineCaptureTestValidate(pool); err == nil || !strings.Contains(err.Error(), "DDL drifted") {
				t.Fatalf("actual DDL drift was accepted: %v", err)
			}
		})
	}
}
