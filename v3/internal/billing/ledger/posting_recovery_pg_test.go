//go:build pgintegration

package ledger

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestPostingHoldRecoversAfterSQLErrorRollsBackTransaction(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 7, 1000)
	// SQL rejects this kind after the Redis hold is acquired.
	_, err := NewPoster(pool, rdb).Post(ctx, billing.Entry{AccountID: account, Amount: -400, Kind: "invalid-kind", OperationID: "sql-failure"})
	if err == nil {
		t.Fatal("invalid ledger kind admitted")
	}
	if postingHeld(t, rdb, account) != 400 {
		t.Fatal("failed transaction did not preserve hold for verified cleanup")
	}
	if n, err := billing.SweepPostingHolds(ctx, rdb, NewAccounts(pool), time.Now().Add(time.Hour), 100); err != nil || n != 1 {
		t.Fatalf("aborted SQL hold not cleaned: %d %v", n, err)
	}
	if bal, ver := pgBalance(t, pool, account); bal != 1000 || ver != 0 || postingHeld(t, rdb, account) != 0 {
		t.Fatalf("failed posting changed balance: %d v%d", bal, ver)
	}
}

func TestPostingRejectsVersionOverflowBeforeReserving(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 7, 1000)
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.accounts SET version=$1 WHERE id=$2`, int64(math.MaxInt64), account); err != nil {
		t.Fatal(err)
	}
	_, err := NewPoster(pool, rdb).Post(ctx, billing.Entry{AccountID: account, Amount: -400, Kind: "transfer", OperationID: "overflow"})
	if err == nil {
		t.Fatal("account version wrapped")
	}
	if exists, _ := rdb.Exists(ctx, billing.BalanceKey(account)).Result(); exists != 0 {
		t.Fatal("invalid posting created a Redis hold")
	}
	if bal, ver := pgBalance(t, pool, account); bal != 1000 || ver != math.MaxInt64 {
		t.Fatalf("overflow changed PG: %d v%d", bal, ver)
	}
}

func TestReconcileRetainsUnpostedUsageWhenBusinessVersionsCoincide(t *testing.T) {
	for _, amount := range []int64{500, -400} {
		t.Run(fmt.Sprint(amount), func(t *testing.T) {
			pool, rdb := testPool(t), testRedis(t)
			account := fundedAccount(t, pool, 7, 1000)
			setHot(t, rdb, account, "balance", 900, "reserved", 0, "ver", 1, "base", 0)
			if _, err := NewPoster(pool, rdb).Post(ctx, billing.Entry{AccountID: account, Amount: credits.Micro(amount), Kind: "transfer", OperationID: "business"}); err != nil {
				t.Fatal(err)
			}
			reconciler := NewReconciler(pool, rdb, quiet)
			result, err := reconciler.Run(ctx)
			if err != nil || result.InFlight != 1 || result.Drifted != 0 || result.Repaired != 0 {
				t.Fatalf("business in flight treated as drift: %+v %v", result, err)
			}
			if h := hotOf(t, rdb, account); h.balance != 900 || h.ver != 1 {
				t.Fatalf("reconcile forgave unposted usage: %+v", h)
			}
			if _, err := NewBalanceRelay(pool, rdb, quiet).Step(ctx); err != nil {
				t.Fatal(err)
			}
			xadd(t, rdb, charge(account, "usage-before-business", 100)...)
			drain(t, newWorker(t, pool, rdb, WorkerConfig{}))
			result, err = reconciler.Run(ctx)
			if err != nil || result.Checked != 1 || result.NeedsAttention() {
				t.Fatalf("after delivery/drain: %+v %v", result, err)
			}
			if h := hotOf(t, rdb, account); h.balance != 900+amount || h.ver != 2 {
				t.Fatalf("business/gateway money lost: %+v", h)
			}
		})
	}
}

func TestReconcileRepairRejectsPGPostingAfterSnapshot(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 7, 1000)
	setHot(t, rdb, account, "balance", 900, "reserved", 0, "ver", 0, "base", 0)
	reconciler := NewReconciler(pool, rdb, quiet)
	rows, err := reconciler.pageAfter(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	h := hotOf(t, rdb, account)
	if _, err := NewPoster(pool, rdb).Post(ctx, billing.Entry{AccountID: account, Amount: 500, Kind: "topup", OperationID: "after-snapshot"}); err != nil {
		t.Fatal(err)
	}
	var result ReconcileResult
	if err := reconciler.repair(ctx, rows[0], h, "drift", &result); err != nil || result.Repaired != 0 {
		t.Fatalf("stale PG snapshot repaired: %+v %v", result, err)
	}
	if current := hotOf(t, rdb, account); current.balance != 900 || current.ver != 0 {
		t.Fatalf("stale repair changed Redis: %+v", current)
	}
}
