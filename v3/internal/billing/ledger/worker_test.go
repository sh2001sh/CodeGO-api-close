//go:build pgintegration

package ledger

import (
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func testRedis(t *testing.T) *redisx.Client {
	t.Helper()
	addr := os.Getenv("V3_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("V3_TEST_REDIS_ADDR not set")
	}
	rdb, err := redisx.Connect(redisx.Config{Addr: addr, PoolSize: 512})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	return rdb
}

func newWorker(t *testing.T, pool *pgxpool.Pool, rdb *redisx.Client, cfg WorkerConfig) *Worker {
	t.Helper()
	if cfg.Consumer == "" {
		cfg.Consumer = "test"
	}
	cfg.Block = 50 * time.Millisecond
	w, err := NewWorker(pool, rdb, cfg, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.ensureGroup(ctx); err != nil {
		t.Fatal(err)
	}
	return w
}

// drain steps until a step acknowledges nothing.
func drain(t *testing.T, w *Worker) int {
	t.Helper()
	total := 0
	for {
		n, err := w.Step(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return total
		}
		total += n
	}
}

func fundedAccount(t *testing.T, pool *pgxpool.Pool, userID, balance int64) int64 {
	t.Helper()
	id, err := NewAccounts(pool).WalletAccount(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.accounts SET balance = $1 WHERE id = $2`, balance, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func pgBalance(t *testing.T, pool *pgxpool.Pool, id int64) (balance, version int64) {
	t.Helper()
	if err := pool.QueryRow(ctx, `SELECT balance, version FROM v3_billing.accounts WHERE id = $1`, id).Scan(&balance, &version); err != nil {
		t.Fatal(err)
	}
	return balance, version
}

// Acceptance (tasks.md M2): the real settler's events, posted by the worker,
// leave PostgreSQL and Redis agreeing to the micro-credit.
func TestWorkerLedgerMatchesRedis(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	accounts := NewAccounts(pool)
	acct := fundedAccount(t, pool, 7, 1_000_000)
	snap := &catalog.Snapshot{
		Groups: map[string]catalog.Group{"default": {Multiplier: 1}},
		Prices: map[string]catalog.Price{"gpt": {Mode: "per_token", InputPerMTok: 1_000_000, OutputPerMTok: 2_000_000}},
	}
	settler, err := billing.New(rdb, func() *catalog.Snapshot { return snap }, accounts, accounts, billing.Config{}, quiet)
	if err != nil {
		t.Fatal(err)
	}

	const n = 1000
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := &gateway.Request{ID: "r" + strconv.Itoa(i), Model: "gpt", Body: []byte(`{"max_tokens":100}`),
				Principal: gateway.Principal{UserID: 7, Group: "default"}}
			if err := settler.Reserve(ctx, req); err != nil {
				t.Error(err)
				return
			}
			out := gateway.Outcome{Terminal: gateway.TerminalCompleted, Charge: true, Usage: gateway.Usage{PromptTokens: 10, CompletionTokens: 20}}
			if err := settler.Finalize(ctx, req, out); err != nil { // 10 + 40 = 50
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()

	w := newWorker(t, pool, rdb, WorkerConfig{Batch: 128})
	if got := drain(t, w); got != n {
		t.Fatalf("acknowledged %d events; want %d", got, n)
	}
	bal, ver := pgBalance(t, pool, acct)
	redisBal, _ := rdb.HGet(ctx, redisx.KeyBalancePrefix+"{"+strconv.FormatInt(acct, 10)+"}", "balance").Int64()
	if want := int64(1_000_000 - 50*n); bal != want || redisBal != want || ver != n {
		t.Fatalf("pg balance=%d v%d, redis=%d; want %d v%d on both", bal, ver, redisBal, want, n)
	}
	var entries, sum, lastAfter int64
	if err := pool.QueryRow(ctx, `SELECT count(*), sum(amount), (SELECT balance_after FROM v3_billing.ledger_entries WHERE account_id = $1 ORDER BY id DESC LIMIT 1)
		FROM v3_billing.ledger_entries WHERE account_id = $1`, acct).Scan(&entries, &sum, &lastAfter); err != nil {
		t.Fatal(err)
	}
	if entries != n || sum != -50*n || lastAfter != bal {
		t.Fatalf("ledger entries=%d sum=%d last balance_after=%d", entries, sum, lastAfter)
	}
}

func xadd(t *testing.T, rdb *redisx.Client, fields ...any) {
	t.Helper()
	if err := rdb.XAdd(ctx, &redis.XAddArgs{Stream: redisx.StreamBillingEvents, Values: fields}).Err(); err != nil {
		t.Fatal(err)
	}
}

func charge(acct int64, req string, amount int64) []any {
	return []any{billing.FieldRequestID, req, billing.FieldAccountID, strconv.FormatInt(acct, 10),
		billing.FieldAmount, strconv.FormatInt(amount, 10), billing.FieldTerminal, "completed"}
}

func count(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM v3_billing."+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestReplayConflictsAndDeadLetters(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	acct := fundedAccount(t, pool, 1, 10_000)
	w := newWorker(t, pool, rdb, WorkerConfig{})

	xadd(t, rdb, charge(acct, "a", 100)...)
	xadd(t, rdb, charge(acct, "a", 100)...) // redelivered duplicate: same fields
	xadd(t, rdb, charge(acct, "a", 999)...) // same request, different charge: conflict
	xadd(t, rdb, charge(acct, "release", 0)...)
	xadd(t, rdb, charge(9999, "orphan", 5)...) // unknown account
	xadd(t, rdb, billing.FieldRequestID, "broken", billing.FieldAmount, "x")
	xadd(t, rdb, charge(acct, "b", 50)...)
	if got := drain(t, w); got != 7 {
		t.Fatalf("acknowledged %d; want all 7 so the stream never blocks", got)
	}
	if bal, ver := pgBalance(t, pool, acct); bal != 10_000-150 || ver != 2 {
		t.Fatalf("balance=%d v%d; want 9850 v2 (a and b once)", bal, ver)
	}
	if c, d := count(t, pool, "dedup_conflicts"), count(t, pool, "dead_letters"); c != 1 || d != 2 {
		t.Fatalf("conflicts=%d dead letters=%d; want 1 and 2", c, d)
	}

	// Re-posting the same batch after a crash (commit done, ack lost) is a no-op.
	e, _ := parseEvent("1-1", map[string]any{billing.FieldRequestID: "b", billing.FieldAccountID: strconv.FormatInt(acct, 10),
		billing.FieldAmount: "50", billing.FieldTerminal: "completed"})
	res, err := post(ctx, pool, []event{e}, nil)
	if err != nil || res.duplicates != 1 || res.posted != 0 {
		t.Fatalf("replay = %+v, %v", res, err)
	}
	if bal, _ := pgBalance(t, pool, acct); bal != 9850 {
		t.Fatalf("replay changed balance to %d", bal)
	}
}

func TestReclaimFromDeadConsumer(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	acct := fundedAccount(t, pool, 1, 10_000)
	// Wide enough that the "not yet" check below cannot race past it under -race.
	w := newWorker(t, pool, rdb, WorkerConfig{ClaimIdle: 500 * time.Millisecond})
	for i := 0; i < 5; i++ {
		xadd(t, rdb, charge(acct, "r"+strconv.Itoa(i), 10)...)
	}
	// A consumer reads the entries and dies before acknowledging.
	if err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{Group: redisx.GroupLedger, Consumer: "dead",
		Streams: []string{redisx.StreamBillingEvents, ">"}, Count: 10}).Err(); err != nil {
		t.Fatal(err)
	}
	if got := drain(t, w); got != 0 {
		t.Fatalf("worker took %d entries still owned by a live-looking consumer", got)
	}
	time.Sleep(600 * time.Millisecond)
	if got := drain(t, w); got != 5 {
		t.Fatalf("reclaimed %d; want 5", got)
	}
	if bal, _ := pgBalance(t, pool, acct); bal != 10_000-50 {
		t.Fatalf("balance = %d; want 9950", bal)
	}
	if p, _ := rdb.XPending(ctx, redisx.StreamBillingEvents, redisx.GroupLedger).Result(); p.Count != 0 {
		t.Fatalf("pending = %d after reclaim", p.Count)
	}
}

func TestTrimRespectsEveryGroup(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	acct := fundedAccount(t, pool, 1, 10_000)
	w := newWorker(t, pool, rdb, WorkerConfig{})
	for i := 0; i < 20; i++ {
		xadd(t, rdb, charge(acct, "r"+strconv.Itoa(i), 1)...)
	}
	// A second consumer (future audit projection) that has read nothing.
	if err := rdb.XGroupCreate(ctx, redisx.StreamBillingEvents, "audit", "0").Err(); err != nil {
		t.Fatal(err)
	}
	drain(t, w)
	if err := w.Trim(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := rdb.XLen(ctx, redisx.StreamBillingEvents).Result(); n != 20 {
		t.Fatalf("trim removed entries the audit group has not read: len=%d", n)
	}
	if err := rdb.XGroupDestroy(ctx, redisx.StreamBillingEvents, "audit").Err(); err != nil {
		t.Fatal(err)
	}
	xadd(t, rdb, charge(acct, "late", 1)...) // unread by the ledger group: must survive
	if err := w.Trim(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := rdb.XLen(ctx, redisx.StreamBillingEvents).Result(); n != 1 {
		t.Fatalf("len after trim = %d; want only the undelivered entry", n)
	}
	if got := drain(t, w); got != 1 {
		t.Fatalf("undelivered entry lost by trim: acked %d", got)
	}
}
