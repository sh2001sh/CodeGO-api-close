//go:build pgintegration

package ledger

import (
	"errors"
	"math"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestPostAccrualBatchIdempotencyAccountRestrictionsAndOverflow(t *testing.T) {
	pool := testPool(t)
	var account int64
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',7,'marketplace_pending') RETURNING id`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	p := NewPoster(pool)
	entry := billing.Entry{AccountID: account, Amount: 5, Kind: "marketplace_accrue", OperationID: "batch-entry", RequestID: "batch-request", Metadata: map[string]any{"source": "market", "order": 1}}
	apply := func(entries []billing.Entry) error {
		return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return p.PostAccrualsTx(ctx, tx, entries, nil) })
	}
	if err := apply([]billing.Entry{entry, entry}); err != nil {
		t.Fatal(err)
	}
	if err := apply([]billing.Entry{entry}); err != nil {
		t.Fatal(err)
	}
	if balance, version := pgBalance(t, pool, account); balance != 5 || version != 1 || count(t, pool, "ledger_entries") != 1 || count(t, pool, "balance_outbox") != 1 {
		t.Fatalf("replay=%d/%d", balance, version)
	}
	fresh, altered := entry, entry
	fresh.OperationID = "batch-fresh"
	altered.Amount++
	if err := apply([]billing.Entry{fresh, altered}); !errors.Is(err, billing.ErrPostConflict) {
		t.Fatalf("altered replay=%v", err)
	}
	if err := apply([]billing.Entry{fresh, entry, altered}); !errors.Is(err, billing.ErrPostConflict) {
		t.Fatalf("altered in-batch replay=%v", err)
	}
	if got := count(t, pool, "ledger_entries"); got != 1 {
		t.Fatalf("partial writes=%d", got)
	}
	wallet := fundedAccount(t, pool, 8, 100)
	fresh.AccountID = wallet
	if err := apply([]billing.Entry{fresh}); err == nil {
		t.Fatal("wallet credit bypassed funding provenance")
	}
	fresh.AccountID, fresh.Amount = account, -1
	if err := apply([]billing.Entry{fresh}); err == nil {
		t.Fatal("debit bypassed posting reservation")
	}
	fresh.Amount = 1
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.accounts SET balance=$2 WHERE id=$1`, account, int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	if err := apply([]billing.Entry{fresh}); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("balance overflow=%v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.accounts SET balance=5,version=$2 WHERE id=$1`, account, int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	if err := apply([]billing.Entry{fresh}); err == nil {
		t.Fatal("version overflow accepted")
	}
	if got := count(t, pool, "ledger_entries"); got != 1 {
		t.Fatalf("failed posts committed=%d", got)
	}
}
