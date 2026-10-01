//go:build pgintegration

package ledger

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func setHot(t *testing.T, rdb *redisx.Client, acct int64, fields ...any) {
	t.Helper()
	if err := rdb.HSet(ctx, billing.BalanceKey(acct), fields...).Err(); err != nil {
		t.Fatal(err)
	}
}

func hotOf(t *testing.T, rdb *redisx.Client, acct int64) hot {
	t.Helper()
	h, ok := parseHot(rdb.HMGet(ctx, billing.BalanceKey(acct), "balance", "ver", "base").Val())
	if !ok {
		t.Fatalf("account %d has no hot balance", acct)
	}
	return h
}

func TestReconcileClassifiesAndRepairs(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	ok := fundedAccount(t, pool, 1, 1000)
	drift := fundedAccount(t, pool, 2, 1000)
	flight := fundedAccount(t, pool, 3, 1000)
	fundedAccount(t, pool, 4, 1000) // no Redis hash
	behind := fundedAccount(t, pool, 5, 700)
	stuck := fundedAccount(t, pool, 6, 700)
	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE v3_billing.accounts SET version = 3 WHERE owner_id IN (1, 2, 3)`)
	exec(`UPDATE v3_billing.accounts SET version = 5 WHERE owner_id IN (5, 6)`)

	setHot(t, rdb, ok, "balance", 1000, "reserved", 0, "ver", 3, "base", 0)
	setHot(t, rdb, drift, "balance", 900, "reserved", 40, "ver", 3, "base", 0)
	setHot(t, rdb, flight, "balance", 950, "reserved", 0, "ver", 4, "base", 0)
	setHot(t, rdb, behind, "balance", 1000, "reserved", 0, "ver", 3, "base", 3) // reloaded at v3, missed 2 charges
	setHot(t, rdb, stuck, "balance", 990, "reserved", 0, "ver", 4, "base", 3)   // also charged once since reload

	res, err := NewReconciler(pool, rdb, quiet).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := ReconcileResult{Checked: 2, InFlight: 1, Missing: 1, Drifted: 1, Behind: 2, Repaired: 2, Stuck: 1}
	if res != want || !res.NeedsAttention() {
		t.Fatalf("result = %+v; want %+v", res, want)
	}
	if h := hotOf(t, rdb, drift); h.balance != 1000 || h.ver != 3 {
		t.Fatalf("drift not repaired: %+v", h)
	}
	if held, _ := rdb.HGet(ctx, billing.BalanceKey(drift), "reserved").Int64(); held != 40 {
		t.Fatalf("repair touched holds: reserved=%d", held)
	}
	if h := hotOf(t, rdb, behind); h.balance != 700 || h.ver != 5 || h.base != 5 {
		t.Fatalf("behind not reset to ledger: %+v", h)
	}
	if h := hotOf(t, rdb, flight); h.balance != 950 {
		t.Fatal("in-flight account was modified")
	}
	if h := hotOf(t, rdb, stuck); h.balance != 990 {
		t.Fatal("stuck account must be left for an operator")
	}
}

// The repair must not overwrite a charge that lands after the comparison.
func TestRepairIsFencedByVersion(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	acct := fundedAccount(t, pool, 1, 1000)
	setHot(t, rdb, acct, "balance", 1000, "ver", 4, "base", 0) // a charge landed: ver 3 -> 4
	n, err := repairScript.Run(ctx, rdb, []string{billing.BalanceKey(acct)}, 3, 1000, 3).Int()
	if err != nil || n != 0 {
		t.Fatalf("stale repair applied: n=%d err=%v", n, err)
	}
}

// Reconciling continuously during live traffic must never report drift: the
// version check has to tell in-flight charges from real disagreement.
func TestReconcileUnderLiveTrafficHasNoFalseDrift(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	accounts := NewAccounts(pool)
	accts := make([]int64, 20)
	for i := range accts {
		accts[i] = fundedAccount(t, pool, int64(i+1), 1_000_000)
	}
	snap := &catalog.Snapshot{
		Groups: map[string]catalog.Group{"default": {Multiplier: 1}},
		Prices: map[string]catalog.Price{"gpt": {Mode: "per_token", InputPerMTok: 1_000_000, OutputPerMTok: 2_000_000}},
	}
	settler, err := billing.New(rdb, func() *catalog.Snapshot { return snap }, accounts, accounts, billing.Config{}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	w := newWorker(t, pool, rdb, WorkerConfig{Batch: 50})
	rec := NewReconciler(pool, rdb, quiet)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var reqs atomic.Int64
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				req := &gateway.Request{ID: "g" + strconv.Itoa(g) + "-" + strconv.Itoa(i), Model: "gpt", Body: []byte(`{"max_tokens":10}`),
					Principal: gateway.Principal{UserID: int64(i%len(accts) + 1), Group: "default"}}
				if err := settler.Reserve(ctx, req); err != nil {
					t.Error(err)
					return
				}
				out := gateway.Outcome{Terminal: gateway.TerminalCompleted, Charge: true, Usage: gateway.Usage{PromptTokens: 5, CompletionTokens: 5}}
				if err := settler.Finalize(ctx, req, out); err != nil {
					t.Error(err)
					return
				}
				reqs.Add(1)
			}
		}(g)
	}
	wg.Add(1)
	go func() { // the ledger worker keeps posting throughout
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := w.Step(ctx); err != nil {
				t.Error(err)
				return
			}
		}
	}()

	var passes, checked int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		res, err := rec.Run(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if res.Drifted != 0 || res.Behind != 0 {
			t.Fatalf("false alarm under live traffic: %+v", res)
		}
		passes++
		checked += res.Checked
	}
	close(stop)
	wg.Wait()
	drain(t, w)
	final, err := rec.Run(ctx)
	if err != nil || final.Checked != len(accts) || final.NeedsAttention() {
		t.Fatalf("after drain: %+v, %v; want all %d accounts checked and equal", final, err, len(accts))
	}
	t.Logf("%d requests, %d reconcile passes, %d exact comparisons during traffic", reqs.Load(), passes, checked)
}
