// Command seed prepares a disposable database for end-to-end benchmarks:
// it applies the v3 migrations from scratch and creates one channel pointing
// at the mock upstream, a price, and N users each with one API key and a
// funded wallet. Keys are written as a JSON array for k6.
//
//	V3_PG_DSN=... V3_SECRET_KEY=... seed -reset -upstream http://mock:18080/m/complete -users 200 -keys keys.json
//
// -reset drops every v3_* schema. Never point it at a real database.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/migrations"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func main() {
	reset := flag.Bool("reset", false, "drop and recreate every v3 schema (disposable databases only)")
	upstream := flag.String("upstream", "http://127.0.0.1:18080/m/complete", "mock upstream base URL")
	users := flag.Int("users", 100, "number of users, one key each")
	balance := flag.Int64("balance", 1<<50, "wallet balance per user, micro-credits")
	keysFile := flag.String("keys", "keys.json", "where to write the generated API keys")
	flag.Parse()
	if err := run(context.Background(), *reset, *upstream, *users, *balance, *keysFile); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, reset bool, upstream string, users int, balance int64, keysFile string) error {
	if !reset {
		return errors.New("seed only runs with -reset on a disposable database")
	}
	crypto, err := catalog.NewAESGCMFromBase64(os.Getenv("V3_SECRET_KEY"))
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, os.Getenv("V3_PG_DSN"))
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := migrate(ctx, pool); err != nil {
		return err
	}
	if err := seedCatalog(ctx, pool, crypto, upstream); err != nil {
		return err
	}
	keys, err := seedUsers(ctx, pool, users, balance)
	if err != nil {
		return err
	}
	blob, _ := json.Marshal(keys)
	if err := os.WriteFile(keysFile, blob, 0o600); err != nil {
		return err
	}
	fmt.Printf("seeded 1 channel and %d users; keys written to %s\n", users, keysFile)
	return nil
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	for _, s := range []string{"v3_billing", "v3_catalog", "v3_identity", "v3_platform"} {
		if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+s+" CASCADE"); err != nil {
			return err
		}
	}
	names, err := migrations.Files()
	if err != nil {
		return err
	}
	for _, n := range names {
		sql, _ := migrations.Read(n)
		if _, err := pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("apply %s: %w", n, err)
		}
	}
	return nil
}

func seedCatalog(ctx context.Context, pool *pgxpool.Pool, crypto *catalog.AESGCM, upstream string) error {
	secret, err := crypto.Encrypt([]byte("sk-upstream-mock"))
	if err != nil {
		return err
	}
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO v3_catalog.groups (name, multiplier) VALUES ('default', 1)`, nil},
		{`INSERT INTO v3_catalog.channels (id, name, provider, base_url) OVERRIDING SYSTEM VALUE
		  VALUES (1, 'mock', 'openai', $1)`, []any{upstream}},
		{`INSERT INTO v3_catalog.channel_credentials (channel_id, secret) VALUES (1, $1)`, []any{secret}},
		{`INSERT INTO v3_catalog.channel_groups (channel_id, group_name) VALUES (1, 'default')`, nil},
		{`INSERT INTO v3_catalog.channel_models (channel_id, model) VALUES (1, 'mock')`, nil},
		{`INSERT INTO v3_catalog.model_prices (model, input_per_mtok, output_per_mtok) VALUES ('mock', 1000000, 2000000)`, nil},
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s.sql, s.args...); err != nil {
			return fmt.Errorf("seed catalog: %w", err)
		}
	}
	return nil
}

func seedUsers(ctx context.Context, pool *pgxpool.Pool, n int, balance int64) ([]string, error) {
	keys := make([]string, 0, n)
	batch := &pgx.Batch{}
	for i := 1; i <= n; i++ {
		key, hash, prefix, err := identity.GenerateKey()
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
		batch.Queue(`INSERT INTO v3_identity.users (id, username) VALUES ($1, $2)`, i, fmt.Sprintf("bench%d", i))
		batch.Queue(`INSERT INTO v3_identity.api_keys (user_id, key_hash, key_prefix, key_ciphertext) VALUES ($1, $2, $3, '\x00')`,
			i, hash[:], prefix)
		batch.Queue(`INSERT INTO v3_billing.accounts (owner_type, owner_id, kind) VALUES ('user', $1, 'wallet')`, i)
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return err
		}
		poster := ledger.NewPoster(pool)
		for i := 1; i <= n; i++ {
			var account int64
			if err := tx.QueryRow(ctx, `SELECT id FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=$1 AND kind='wallet'`, i).Scan(&account); err != nil {
				return err
			}
			if _, err := poster.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: credits.Micro(balance), Kind: "opening",
				OperationID: fmt.Sprintf("bench-opening:%d", i), Reason: "benchmark_fixture"}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("seed users: %w", err)
	}
	return keys, nil
}
