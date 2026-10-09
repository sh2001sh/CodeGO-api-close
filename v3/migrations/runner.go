package migrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const EmbeddedSchemaLock int64 = 738301030
const ExactPriceMigration = "20261010000108_channelmarket_exact_prices.sql"

// ApplyEmbeddedTx shares the existing embedded runner with the guarded empty
// online upgrade. The caller owns commit, so schema and ownership proof commit
// together. Atlas installations are never silently adopted.
func ApplyEmbeddedTx(ctx context.Context, tx pgx.Tx) (applied, total int, err error) {
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, EmbeddedSchemaLock); err != nil {
		return
	}
	var installed, tracked bool
	if err = tx.QueryRow(ctx, `SELECT to_regclass('v3_identity.users') IS NOT NULL,
	 to_regclass('v3_platform.embedded_schema_revisions') IS NOT NULL`).Scan(&installed, &tracked); err != nil {
		return
	}
	if installed && !tracked {
		err = errors.New("schema already exists without embedded revisions; use its existing Atlas migration workflow")
		return
	}
	if _, err = tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS v3_platform;
	 CREATE TABLE IF NOT EXISTS v3_platform.embedded_schema_revisions(name text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return
	}
	var names []string
	names, err = Files()
	if err != nil {
		return
	}
	total = len(names)
	for _, name := range names {
		var source string
		source, err = Read(name)
		if err != nil {
			return
		}
		hash := sha256.Sum256([]byte(source))
		checksum := hex.EncodeToString(hash[:])
		var previous string
		err = tx.QueryRow(ctx, `SELECT checksum FROM v3_platform.embedded_schema_revisions WHERE name=$1`, name).Scan(&previous)
		if err == nil {
			if previous != checksum {
				err = fmt.Errorf("migration %s changed after application", name)
				return
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return
		}
		if _, err = tx.Exec(ctx, source); err != nil {
			err = fmt.Errorf("apply %s: %w", name, err)
			return
		}
		if _, err = tx.Exec(ctx, `INSERT INTO v3_platform.embedded_schema_revisions(name,checksum) VALUES($1,$2)`, name, checksum); err != nil {
			return
		}
		applied++
	}
	err = nil
	return
}
