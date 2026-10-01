//go:build pgintegration

package ledger

import (
	"strconv"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// Two workers in the same group share the load without double posting or
// deadlocking on account locks.
func TestConcurrentWorkers(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	accts := make([]int64, 10)
	for i := range accts {
		accts[i] = fundedAccount(t, pool, int64(i+1), 1_000_000)
	}
	const n = 2000
	pipe := rdb.Pipeline()
	for i := 0; i < n; i++ {
		pipe.XAdd(ctx, &redis.XAddArgs{Stream: redisx.StreamBillingEvents, Values: charge(accts[i%len(accts)], "r"+strconv.Itoa(i), 3)})
	}
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatal(err)
	}
	workers := []*Worker{newWorker(t, pool, rdb, WorkerConfig{Consumer: "w1", Batch: 64}), newWorker(t, pool, rdb, WorkerConfig{Consumer: "w2", Batch: 64})}
	var wg sync.WaitGroup
	acked := make([]int, len(workers))
	for i, w := range workers {
		wg.Add(1)
		go func(i int, w *Worker) {
			defer wg.Done()
			acked[i] = drain(t, w)
		}(i, w)
	}
	wg.Wait()
	if acked[0]+acked[1] != n || acked[0] == 0 || acked[1] == 0 {
		t.Fatalf("acked %v; want %d split across both workers", acked, n)
	}
	for _, id := range accts {
		per := int64(n / len(accts))
		if bal, ver := pgBalance(t, pool, id); bal != 1_000_000-3*per || ver != per {
			t.Fatalf("account %d balance=%d v%d", id, bal, ver)
		}
	}
	if got := count(t, pool, "ledger_entries"); got != n {
		t.Fatalf("ledger entries = %d; want %d", got, n)
	}
}
