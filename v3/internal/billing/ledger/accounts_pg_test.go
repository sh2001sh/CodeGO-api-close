//go:build pgintegration

package ledger

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
	var schemas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		schemas = append(schemas, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, s := range schemas {
		if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{s}.Sanitize()+" CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	names, _ := migrations.Files()
	for _, n := range names {
		sql, _ := migrations.Read(n)
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("apply %s: %v", n, err)
		}
	}
	return pool
}

// Concurrent first requests for one user must agree on a single wallet.
func TestWalletAccountIsCreatedOnceUnderConcurrency(t *testing.T) {
	pool := testPool(t)
	ids := make([]int64, 50)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, err := NewAccounts(pool).WalletAccount(ctx, 7) // separate caches: every call reaches PG
			if err != nil {
				t.Error(err)
			}
			ids[i] = id
		}(i)
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] || id == 0 {
			t.Fatalf("wallet ids differ: %v", ids)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.accounts WHERE owner_id = 7`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("accounts rows = %d, %v; want 1", n, err)
	}
}

func TestLedgerBalance(t *testing.T) {
	pool := testPool(t)
	a := NewAccounts(pool)
	id, err := a.WalletAccount(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.accounts SET balance = 2000000, version = 3 WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	bal, ver, err := a.LedgerBalance(ctx, id)
	if err != nil || bal != 2_000_000 || ver != 3 {
		t.Fatalf("LedgerBalance = %d v%d, %v", bal, ver, err)
	}
	if _, _, err := a.LedgerBalance(ctx, 999); !errors.Is(err, ErrUnknownAccount) {
		t.Fatalf("unknown account err = %v", err)
	}
}
