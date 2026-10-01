//go:build pgintegration

package ledger

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

func TestSubscriptionDrainWaitsForHoldsAndUnpostedUsage(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 7, 1000)
	setHot(t, rdb, account, "balance", 900, "reserved", 100, "ver", 1, "base", 0)
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
	if check() {
		t.Fatal("expired while a stream still owns a hold")
	}
	if closed, _ := rdb.HGet(ctx, billing.BalanceKey(account), "closed").Result(); closed != "1" {
		t.Fatal("new funding admission was not closed")
	}
	setHot(t, rdb, account, "reserved", 0)
	if check() {
		t.Fatal("expired before the usage event reached PG")
	}
	xadd(t, rdb, charge(account, "last-usage", 100)...)
	drain(t, newWorker(t, pool, rdb, WorkerConfig{}))
	if !check() {
		t.Fatal("account never drained after hold and event were completed")
	}
	poster := NewPoster(pool)
	if _, err := poster.Post(ctx, billing.Entry{AccountID: account, Amount: -900, Kind: "subscription_expire", OperationID: "expiry"}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewBalanceRelay(pool, rdb, quiet).Step(ctx); err != nil {
		t.Fatal(err)
	}
	if bal, version := pgBalance(t, pool, account); bal != 0 || version != 2 {
		t.Fatalf("PG expiry=%d v%d", bal, version)
	}
	if h := hotOf(t, rdb, account); h.balance != 0 || h.ver != 2 {
		t.Fatalf("Redis expiry %+v", h)
	}
}
