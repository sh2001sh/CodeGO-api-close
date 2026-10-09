package main

import (
	"context"
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
	applied, total, err := migrations.ApplyEmbeddedTx(ctx, tx)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "schema_migrations_applied=%d total=%d\n", applied, total)
	return err
}
