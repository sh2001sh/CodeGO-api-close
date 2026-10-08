//go:build pgintegration

package ledger

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func marketBatchFixture(t *testing.T, pool *pgxpool.Pool) *channelmarket.Service {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(7,'market-batch-consumer'),(8,'market-batch-owner');
	 INSERT INTO v3_catalog.groups(name,multiplier) VALUES('market-batch',1);
	 INSERT INTO v3_catalog.channels(id,name,provider,base_url,scope,owner_user_id) OVERRIDING SYSTEM VALUE VALUES(3,'market-batch','openai','https://example.com','marketplace',8);
	 INSERT INTO v3_channelmarket.groups(id,public_channel_id,channel_id,owner_user_id,public_slug,internal_group_name,display_name,visibility,lifecycle_status)
	 VALUES('market-batch','3',3,8,'market-batch','market-batch','market batch','public','active')`)
	if err != nil {
		t.Fatal(err)
	}
	return channelmarket.New(pool, nil, NewPoster(pool), channelmarket.Config{}, quiet)
}

func marketBatchEvent(t *testing.T, account int64, request string) event {
	t.Helper()
	e := usageHookEvent(t, account, request, 1000, false)
	e.fields["marketplace_gross_micro"], e.fields["marketplace_multiplier_ppm"] = "1000", "1000000"
	e.fingerprint = fingerprint(e.fields)
	return e
}

func TestMarketIncomeBatchHookRollbackAndFreshEventsOnly(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 10000)
	market := marketBatchFixture(t, pool)
	e := marketBatchEvent(t, account, "batch-hook")
	failed := func(ctx context.Context, tx pgx.Tx, fields map[string]string) error {
		return errors.New("later guard failed")
	}
	if _, err := postWithMarketplaceBatch(ctx, pool, []event{e}, nil, nil, market.AccrueUsageBatchTx, failed); err == nil {
		t.Fatal("later hook failure committed batch credits")
	}
	if balance, version := pgBalance(t, pool, account); balance != 10000 || version != 0 || count(t, pool, "ledger_entries") != 0 || count(t, pool, "billing_dedup") != 0 || count(t, pool, "balance_outbox") != 0 {
		t.Fatalf("partial commit=%d/%d", balance, version)
	}
	var settlements int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_channelmarket.settlements`).Scan(&settlements); err != nil || settlements != 0 {
		t.Fatalf("partial settlements=%d err=%v", settlements, err)
	}
	for range 2 {
		if _, err := postWithMarketplaceBatch(ctx, pool, []event{e}, nil, nil, market.AccrueUsageBatchTx); err != nil {
			t.Fatal(err)
		}
	}
	if balance, version := pgBalance(t, pool, account); balance != 9000 || version != 1 || count(t, pool, "ledger_entries") != 3 || count(t, pool, "usage_logs") != 1 {
		t.Fatalf("replay committed=%d/%d", balance, version)
	}
	altered := e
	altered.fields = make(map[string]string, len(e.fields))
	for k, v := range e.fields {
		altered.fields[k] = v
	}
	altered.fields["marketplace_gross_micro"] = "1001"
	altered.fingerprint = fingerprint(altered.fields)
	res, err := postWithMarketplaceBatch(ctx, pool, []event{altered}, nil, nil, market.AccrueUsageBatchTx)
	if err != nil || res.conflicts != 1 || count(t, pool, "ledger_entries") != 3 {
		t.Fatalf("altered dedup=%+v err=%v", res, err)
	}
}

func TestConcurrentMarketBatchWorkersHotSellerAndReplay(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 7, 1_000_000)
	market := marketBatchFixture(t, pool)
	const total = 600
	pipe := rdb.Pipeline()
	for i := range total {
		e := marketBatchEvent(t, account, fmt.Sprintf("batch-worker-%d", i))
		values := make(map[string]any, len(e.fields))
		for k, v := range e.fields {
			values[k] = v
		}
		pipe.XAdd(ctx, &redis.XAddArgs{Stream: redisx.StreamBillingEvents, Values: values})
	}
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatal(err)
	}
	workers := make([]*Worker, 4)
	for i := range workers {
		workers[i] = newWorker(t, pool, rdb, WorkerConfig{Consumer: "market-batch-" + strconv.Itoa(i), Batch: 64, UsageBatchHook: market.AccrueUsageBatchTx})
	}
	acked := make([]int, len(workers))
	var wg sync.WaitGroup
	for i, w := range workers {
		wg.Add(1)
		go func(i int, w *Worker) { defer wg.Done(); acked[i] = drain(t, w) }(i, w)
	}
	wg.Wait()
	var acknowledgments int
	for _, n := range acked {
		acknowledgments += n
	}
	if acknowledgments != total {
		t.Fatalf("acked=%v", acked)
	}
	if balance, version := pgBalance(t, pool, account); balance != 1_000_000-total*1000 || version != total {
		t.Fatalf("consumer=%d/%d", balance, version)
	}
	var settlements, drift, pending, platform, ownerVersion, platformVersion int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_channelmarket.settlements`).Scan(&settlements); err != nil || settlements != total {
		t.Fatalf("settlements=%d err=%v", settlements, err)
	}
	if err := pool.QueryRow(ctx, `SELECT balance,version FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=8 AND kind='marketplace_pending'`).Scan(&pending, &ownerVersion); err != nil || pending != total*950 || ownerVersion != total {
		t.Fatalf("owner=%d/%d err=%v", pending, ownerVersion, err)
	}
	if err := pool.QueryRow(ctx, `SELECT balance,version FROM v3_billing.accounts WHERE owner_type='platform' AND kind='platform_revenue'`).Scan(&platform, &platformVersion); err != nil || platform != total*50 || platformVersion != total {
		t.Fatalf("platform=%d/%d err=%v", platform, platformVersion, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.accounts a WHERE balance<>CASE WHEN id=$1 THEN 1000000 ELSE 0 END+(SELECT coalesce(sum(amount),0) FROM v3_billing.ledger_entries WHERE account_id=a.id)`, account).Scan(&drift); err != nil || drift != 0 {
		t.Fatalf("drift=%d err=%v", drift, err)
	}
	if count(t, pool, "usage_logs") != total || count(t, pool, "ledger_entries") != total*3 || count(t, pool, "balance_outbox") != total*2 {
		t.Fatal("missing or duplicated transactions")
	}
	if pending, lag, err := workers[0].Backlog(ctx); err != nil || pending != 0 || lag != 0 {
		t.Fatalf("backlog=%d/%d err=%v", pending, lag, err)
	}
	// Redelivery after the original commit but before a successful ACK must
	// preserve both financial rows and settlement when reclaimed.
	e := marketBatchEvent(t, account, "batch-worker-0")
	if _, err := postWithMarketplaceBatch(ctx, pool, []event{e}, nil, nil, market.AccrueUsageBatchTx); err != nil {
		t.Fatal(err)
	}
	if count(t, pool, "ledger_entries") != total*3 {
		t.Fatal("redelivery duplicated money")
	}
	values := make(map[string]any, len(e.fields))
	for key, value := range e.fields {
		values[key] = value
	}
	if _, err := rdb.XAdd(ctx, &redis.XAddArgs{Stream: redisx.StreamBillingEvents, Values: values}).Result(); err != nil {
		t.Fatal(err)
	}
	if _, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{Group: redisx.GroupLedger, Consumer: "committed-before-crash", Streams: []string{redisx.StreamBillingEvents, ">"}, Count: 1}).Result(); err != nil {
		t.Fatal(err)
	}
	recovering := newWorker(t, pool, rdb, WorkerConfig{Consumer: "recovered-market", Batch: 64, ClaimIdle: time.Millisecond, UsageBatchHook: market.AccrueUsageBatchTx})
	time.Sleep(3 * time.Millisecond)
	if n, err := recovering.Step(ctx); err != nil || n != 1 {
		t.Fatalf("reclaim committed event=%d err=%v", n, err)
	}
	if count(t, pool, "ledger_entries") != total*3 || count(t, pool, "usage_logs") != total {
		t.Fatal("reclaimed committed event duplicated ledger or usage")
	}
	if pending, lag, err := recovering.Backlog(ctx); err != nil || pending != 0 || lag != 0 {
		t.Fatalf("recovered backlog=%d/%d err=%v", pending, lag, err)
	}
}
