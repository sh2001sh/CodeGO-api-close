//go:build pgintegration

package catalogcontrol

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/migrations"
)

func legacyBulkPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("catalog_bulk_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, `CREATE DATABASE `+identifier); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, e := admin.Exec(ctx, `DROP DATABASE `+identifier); e != nil {
			t.Errorf("remove disposable database: %v", e)
		}
		admin.Close()
	})
	isolated := config.Copy()
	isolated.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, isolated)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	names, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range names {
		sql, e := migrations.Read(file)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, sql); e != nil {
			t.Fatalf("apply %s: %v", file, e)
		}
	}
	return pool
}
