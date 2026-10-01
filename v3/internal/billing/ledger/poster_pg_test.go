//go:build pgintegration

package ledger

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func TestBusinessPostConcurrentIdempotencyAndConflict(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 10, 1000)
	p := NewPoster(pool)
	e := billing.Entry{AccountID: account, Amount: 500, Kind: "topup", OperationID: "payment-1", Metadata: map[string]any{"provider": "test"}}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Go(func() {
			if _, err := p.Post(ctx, e); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if bal, version := pgBalance(t, pool, account); bal != 1500 || version != 1 {
		t.Fatalf("balance=%d version=%d", bal, version)
	}
	e.Amount++
	if _, err := p.Post(ctx, e); !errors.Is(err, billing.ErrPostConflict) {
		t.Fatalf("conflicting callback: %v", err)
	}
	e.OperationID = "debit"
	e.Amount = -2000
	e.Kind = "transfer"
	if _, err := p.Post(ctx, e); err == nil {
		t.Fatal("overspend admitted")
	}
	if bal, _ := pgBalance(t, pool, account); bal != 1500 {
		t.Fatal("failed posting changed balance")
	}
}

func TestBusinessPostOutboxSurvivesDeliveryRetryAndReload(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 10, 1000)
	// A usage charge is already in Redis and remains in flight to PG. Topup
	// must add its delta, rather than replace the more recent hot balance.
	setHot(t, rdb, account, "balance", 900, "reserved", 25, "ver", 1, "base", 0)
	p := NewPoster(pool)
	if _, err := p.Post(ctx, billing.Entry{AccountID: account, Amount: 500, Kind: "topup", OperationID: "topup"}); err != nil {
		t.Fatal(err)
	}
	var delivery int64
	if err := pool.QueryRow(ctx, `SELECT id FROM v3_billing.balance_outbox`).Scan(&delivery); err != nil {
		t.Fatal(err)
	}
	marker := redisx.KeyBalancePrefix + "post:" + strconv.FormatInt(delivery, 10)
	// Simulate Redis application followed by a crash before deleting outbox.
	if err := businessBalanceScript.Run(ctx, rdb, []string{billing.BalanceKey(account), marker}, 500, 1, (30 * 24 * time.Hour).Milliseconds()).Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := NewBalanceRelay(pool, rdb, quiet).Step(ctx); err != nil || n != 1 {
		t.Fatalf("relay=%d %v", n, err)
	}
	if h := hotOf(t, rdb, account); h.balance != 1400 || h.ver != 2 {
		t.Fatalf("lost usage or doubled topup: %+v", h)
	}
	if held, _ := rdb.HGet(ctx, billing.BalanceKey(account), "reserved").Int64(); held != 25 {
		t.Fatal("topup changed holds")
	}
	// This account was loaded from PG after another posting committed, so its
	// base version proves that replay must not apply the posting a second time.
	if _, err := p.Post(ctx, billing.Entry{AccountID: account, Amount: 200, Kind: "topup", OperationID: "second"}); err != nil {
		t.Fatal(err)
	}
	setHot(t, rdb, account, "balance", 1700, "ver", 2, "base", 2)
	if _, err := NewBalanceRelay(pool, rdb, quiet).Step(ctx); err != nil {
		t.Fatal(err)
	}
	if h := hotOf(t, rdb, account); h.balance != 1700 || h.ver != 2 {
		t.Fatalf("reloaded posting doubled: %+v", h)
	}
}

func TestUsageLogsAreAtomicIdempotentAndMonthly(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 7, 1000)
	fields := []any{billing.FieldRequestID, "usage", billing.FieldAccountID, account, billing.FieldAmount, 10,
		billing.FieldModel, "gpt", billing.FieldUserID, 7, billing.FieldTimestamp, time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC).UnixMilli()}
	xadd(t, rdb, fields...)
	xadd(t, rdb, fields...)
	w := newWorker(t, pool, rdb, WorkerConfig{})
	drain(t, w)
	var logs, entries int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_billing.usage_logs),(SELECT count(*) FROM v3_billing.ledger_entries)`).Scan(&logs, &entries); err != nil {
		t.Fatal(err)
	}
	if logs != 1 || entries != 1 {
		t.Fatalf("logs=%d ledger=%d", logs, entries)
	}
	// Partition creation must handle the current month's late event already
	// in DEFAULT without losing it, and safely repeat.
	for i := 0; i < 2; i++ {
		if err := EnsureUsagePartitions(ctx, pool, time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	var partition string
	if err := pool.QueryRow(ctx, `SELECT tableoid::regclass::text FROM v3_billing.usage_logs`).Scan(&partition); err != nil {
		t.Fatal(err)
	}
	if partition != "v3_billing.usage_logs_202609" {
		t.Fatalf("partition %s", partition)
	}
	fields[1] = "free"
	fields[5] = 0
	xadd(t, rdb, fields...)
	drain(t, w)
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.usage_logs`).Scan(&logs); err != nil || logs != 2 {
		t.Fatalf("free usage missing: %d %v", logs, err)
	}
}
