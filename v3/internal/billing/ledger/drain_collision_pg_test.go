//go:build pgintegration

package ledger

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

func TestDrainRejectsEqualVersionsWithDifferentMoneyOrPendingOutbox(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 7, 1000)
	checker := NewDrainChecker(rdb)
	check := func() bool {
		t.Helper()
		var drained bool
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			var err error
			drained, err = checker.FreezeAndDrained(ctx, tx, account)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return drained
	}
	// Distinct operations can have equal versions. A PG adjustment and
	// unposted gateway usage must not cancel each other's drain evidence.
	if _, err := NewPoster(pool).Post(ctx, billing.Entry{AccountID: account, Amount: 100, Kind: "adjustment", OperationID: "drain-adjustment"}); err != nil {
		t.Fatal(err)
	}
	setHot(t, rdb, account, "balance", 900, "reserved", 0, "ver", 1, "base", 0)
	if check() {
		t.Fatal("different money and coinciding versions falsely drained")
	}
	// A relay may have applied Redis before crashing ahead of outbox deletion.
	setHot(t, rdb, account, "balance", 1100, "base", 1)
	if check() {
		t.Fatal("pending business delivery falsely drained")
	}
	if _, err := NewBalanceRelay(pool, rdb, quiet).Step(ctx); err != nil {
		t.Fatal(err)
	}
	if !check() {
		t.Fatal("equal balances/versions and empty outbox did not drain")
	}
}
