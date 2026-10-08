//go:build pgintegration

package ledger

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

type gatedMarketplaceRecorder struct {
	wrapped UsageRecorder
	locked  chan int
	resume  <-chan struct{}
	first   bool
}

func TestConcurrentWorkersCommitMarketplaceProgressAndSupplierIncome(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	income := marketBatchFixture(t, pool)
	const userCount, total = 20, 640
	accounts := make([]int64, userCount)
	for i := range accounts {
		user := int64(100 + i)
		if _, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES($1,$2)`, user, fmt.Sprintf("market-progress-%d", user)); err != nil {
			t.Fatal(err)
		}
		accounts[i] = fundedAccount(t, pool, user, 100000)
	}
	actual := marketplace.New(pool, nil, nil, nil, nil, marketplace.Config{})
	pipe := rdb.Pipeline()
	for i := range total {
		index := i % userCount
		if (i/userCount)%2 == 0 {
			index = userCount - 1 - index
		}
		e := marketBatchEvent(t, accounts[index], fmt.Sprintf("actual-market-%d", i))
		e.fields[billing.FieldUserID] = strconv.Itoa(100 + index)
		e.fingerprint = fingerprint(e.fields)
		values := make(map[string]any, len(e.fields))
		for key, value := range e.fields {
			values[key] = value
		}
		pipe.XAdd(ctx, &redis.XAddArgs{Stream: redisx.StreamBillingEvents, Values: values})
	}
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatal(err)
	}
	workers := []*Worker{newWorker(t, pool, rdb, WorkerConfig{Consumer: "actual-progress-a", Batch: 64, Marketplace: actual, UsageBatchHook: income.AccrueUsageBatchTx}), newWorker(t, pool, rdb, WorkerConfig{Consumer: "actual-progress-b", Batch: 64, Marketplace: actual, UsageBatchHook: income.AccrueUsageBatchTx})}
	acked := make([]int, len(workers))
	var wg sync.WaitGroup
	for i, w := range workers {
		wg.Add(1)
		go func(i int, w *Worker) { defer wg.Done(); acked[i] = drain(t, w) }(i, w)
	}
	wg.Wait()
	if acked[0]+acked[1] != total || acked[0] == 0 || acked[1] == 0 {
		t.Fatalf("ACKs=%v", acked)
	}
	for i, account := range accounts {
		if balance, version := pgBalance(t, pool, account); balance != 68000 || version != 32 {
			t.Fatalf("user %d wallet=%d/%d", 100+i, balance, version)
		}
		var usage int64
		if err := pool.QueryRow(ctx, `SELECT usage_micro FROM v3_marketplace.blind_box_zero_hour_states WHERE user_id=$1`, 100+i).Scan(&usage); err != nil || usage != 32000 {
			t.Fatalf("user %d progress=%d err=%v", 100+i, usage, err)
		}
	}
	var pending, platform, settlements int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=8 AND kind='marketplace_pending'`).Scan(&pending); err != nil || pending != total*950 {
		t.Fatalf("supplier=%d err=%v", pending, err)
	}
	if err := pool.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE owner_type='platform' AND kind='platform_revenue'`).Scan(&platform); err != nil || platform != total*50 {
		t.Fatalf("commission=%d err=%v", platform, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_channelmarket.settlements`).Scan(&settlements); err != nil || settlements != total {
		t.Fatalf("settlements=%d err=%v", settlements, err)
	}
	if count(t, pool, "ledger_entries") != total*3 || count(t, pool, "usage_logs") != total || count(t, pool, "billing_dedup") != total || count(t, pool, "balance_outbox") != total*2 {
		t.Fatal("missing or duplicated committed records")
	}
	if pending, lag, err := workers[0].Backlog(ctx); err != nil || pending != 0 || lag != 0 {
		t.Fatalf("backlog=%d/%d err=%v", pending, lag, err)
	}
}

func (r *gatedMarketplaceRecorder) RecordUsageTx(ctx context.Context, tx pgx.Tx, user int64, request string, amount credits.Micro) error {
	if err := r.wrapped.RecordUsageTx(ctx, tx, user, request, amount); err != nil {
		return err
	}
	if !r.first {
		r.first = true
		var pid int
		if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		r.locked <- pid
		select {
		case <-r.resume:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (r *gatedMarketplaceRecorder) RecordDiscountUsageTx(ctx context.Context, tx pgx.Tx, user, prop, channel int64, request string, before, after credits.Micro) error {
	return r.wrapped.RecordDiscountUsageTx(ctx, tx, user, prop, channel, request, before, after)
}

func TestMarketplaceBatchesAcquireUserProgressInNumericOrder(t *testing.T) {
	pool := testPool(t)
	if _, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(2,'progress-user-2'),(10,'progress-user-10')`); err != nil {
		t.Fatal(err)
	}
	accounts := map[int64]int64{2: fundedAccount(t, pool, 2, 10000), 10: fundedAccount(t, pool, 10, 10000)}
	props := map[int64]int64{}
	for _, user := range []int64{2, 10} {
		var prop int64
		if err := pool.QueryRow(ctx, `INSERT INTO v3_marketplace.blind_box_props(user_id,kind,title,status,multiplier_ppm,duration_seconds,remaining_seconds,max_discount_micro,expires_at)
		 VALUES($1,'multiplier','retained progress card','active',500000,3600,3600,10000,now()+interval '1 hour') RETURNING id`, user).Scan(&prop); err != nil {
			t.Fatal(err)
		}
		props[user] = prop
	}
	makeEvent := func(user int64, request string) event {
		e := usageHookEvent(t, accounts[user], request, 1000, false)
		e.fields[billing.FieldUserID] = fmt.Sprint(user)
		e.fields[billing.FieldCardID] = fmt.Sprint(props[user])
		e.fields[billing.FieldCardBefore], e.fields[billing.FieldCardAfter] = "2000", "1000"
		e.fingerprint = fingerprint(e.fields)
		return e
	}
	batchA := []event{makeEvent(2, "progress-a-2"), makeEvent(10, "progress-a-10")}
	batchB := []event{makeEvent(10, "progress-b-10"), makeEvent(2, "progress-b-2")}
	actual := marketplace.New(pool, nil, nil, nil, nil, marketplace.Config{})
	resume := make(chan struct{})
	recorderA := &gatedMarketplaceRecorder{wrapped: actual, locked: make(chan int, 1), resume: resume}
	workCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resultA, resultB := make(chan error, 1), make(chan error, 1)
	go func() { _, err := postWithMarketplace(workCtx, pool, batchA, nil, recorderA); resultA <- err }()
	var pidA int
	select {
	case pidA = <-recorderA.locked:
	case err := <-resultA:
		t.Fatalf("first marketplace callback failed: %v", err)
	case <-workCtx.Done():
		t.Fatal(workCtx.Err())
	}
	go func() { _, err := postWithMarketplace(workCtx, pool, batchB, nil, actual); resultB <- err }()
	waiting := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := pool.QueryRow(workCtx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, pidA).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(resume)
	errA, errB := <-resultA, <-resultB
	if !waiting {
		t.Fatal("second batch never reached PostgreSQL user-state lock wait")
	}
	if errA != nil || errB != nil {
		t.Fatalf("marketplace inter-user lock inversion: A=%v B=%v", errA, errB)
	}
	for _, user := range []int64{2, 10} {
		if balance, version := pgBalance(t, pool, accounts[user]); balance != 8000 || version != 2 {
			t.Fatalf("user %d wallet=%d/%d", user, balance, version)
		}
		var usage, points, discount int64
		if err := pool.QueryRow(ctx, `SELECT usage_micro,points FROM v3_marketplace.blind_box_zero_hour_states WHERE user_id=$1`, user).Scan(&usage, &points); err != nil || usage != 2000 || points != 0 {
			t.Fatalf("user %d progress=%d/%d err=%v", user, usage, points, err)
		}
		if err := pool.QueryRow(ctx, `SELECT used_discount_micro FROM v3_marketplace.blind_box_props WHERE id=$1`, props[user]).Scan(&discount); err != nil || discount != 2000 {
			t.Fatalf("user %d card usage=%d err=%v", user, discount, err)
		}
	}
	if count(t, pool, "ledger_entries") != 4 || count(t, pool, "usage_logs") != 4 || count(t, pool, "billing_dedup") != 4 {
		t.Fatal("missing or duplicated settled events")
	}
	var requests []string
	rows, err := pool.Query(ctx, `SELECT request_id FROM v3_billing.ledger_entries ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var request string
		if err = rows.Scan(&request); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		requests = append(requests, request)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"progress-a-2", "progress-a-10", "progress-b-10", "progress-b-2"}; !reflect.DeepEqual(requests, want) {
		t.Fatalf("financial event order changed: %v want %v", requests, want)
	}
	for _, batch := range [][]event{batchA, batchB} {
		if _, err := postWithMarketplace(ctx, pool, batch, nil, actual); err != nil {
			t.Fatal(err)
		}
	}
	if count(t, pool, "ledger_entries") != 4 {
		t.Fatal("redelivery duplicated money")
	}
}
