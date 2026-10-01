//go:build pgintegration

package legacy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Database fixtures are distinct actual databases in the disposable test
// container, not different schemas that happen to share one transaction.
func migrationDatabase(t *testing.T, dsn, kind string) *pgxpool.Pool {
	t.Helper()
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
	name := "migration_" + kind + "_" + hex.EncodeToString(random)
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
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

func readonlySource(t *testing.T, source *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	role := "migration_read_" + hex.EncodeToString(random)
	quoted := pgx.Identifier{role}.Sanitize()
	if _, err := source.Exec(ctx, "CREATE ROLE "+quoted+" LOGIN PASSWORD 'local-migration-readonly-test'"); err != nil {
		t.Fatal(err)
	}
	rows, err := source.Query(ctx, `SELECT nspname FROM pg_namespace WHERE nspname NOT LIKE 'pg_%' AND nspname<>'information_schema' AND nspname NOT LIKE 'v3_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var schemas []string
	for rows.Next() {
		var schema string
		if err = rows.Scan(&schema); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		schemas = append(schemas, schema)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	for _, schema := range schemas {
		identifier := pgx.Identifier{schema}.Sanitize()
		if _, err = source.Exec(ctx, "GRANT USAGE ON SCHEMA "+identifier+" TO "+quoted); err != nil {
			t.Fatal(err)
		}
		if _, err = source.Exec(ctx, "GRANT SELECT ON ALL TABLES IN SCHEMA "+identifier+" TO "+quoted); err != nil {
			t.Fatal(err)
		}
	}
	config := source.Config().Copy()
	config.ConnConfig.User = role
	config.ConnConfig.Password = "local-migration-readonly-test"
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	return reader
}
