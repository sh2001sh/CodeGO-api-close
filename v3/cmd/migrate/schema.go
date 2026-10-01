package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/migrations"
)

// The embedded schema runner is for disposable test environments and first
// installs. Production may use Atlas instead. Existing Atlas installs are
// rejected rather than guessed to be equivalent or marked as applied.
func applySchema(ctx context.Context, pool *pgxpool.Pool, output io.Writer) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(738301030)`); err != nil {
		return err
	}
	var installed, tracked bool
	if err = tx.QueryRow(ctx, `SELECT to_regclass('v3_identity.users') IS NOT NULL,
		to_regclass('v3_platform.embedded_schema_revisions') IS NOT NULL`).Scan(&installed, &tracked); err != nil {
		return err
	}
	if installed && !tracked {
		return errors.New("schema already exists without embedded revisions; use its existing Atlas migration workflow")
	}
	if _, err = tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS v3_platform;
		CREATE TABLE IF NOT EXISTS v3_platform.embedded_schema_revisions(name text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	names, err := migrations.Files()
	if err != nil {
		return err
	}
	var applied int
	for _, name := range names {
		source, readErr := migrations.Read(name)
		if readErr != nil {
			return readErr
		}
		hash := sha256.Sum256([]byte(source))
		checksum := hex.EncodeToString(hash[:])
		var previous string
		err = tx.QueryRow(ctx, `SELECT checksum FROM v3_platform.embedded_schema_revisions WHERE name=$1`, name).Scan(&previous)
		if err == nil {
			if previous != checksum {
				return fmt.Errorf("migration %s changed after application", name)
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err = tx.Exec(ctx, source); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO v3_platform.embedded_schema_revisions(name,checksum) VALUES($1,$2)`, name, checksum); err != nil {
			return err
		}
		applied++
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "schema_migrations_applied=%d total=%d\n", applied, len(names))
	return err
}
