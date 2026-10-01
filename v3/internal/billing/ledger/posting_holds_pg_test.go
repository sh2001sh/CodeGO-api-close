//go:build pgintegration

package ledger

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func postingHeld(t *testing.T, rdb *redisx.Client, account int64) int64 {
	t.Helper()
	n, err := rdb.HGet(ctx, billing.BalanceKey(account), "reserved").Int64()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBusinessDebitRejectsUnpostedUsageAndGatewayHolds(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 7, 1000)
	poster := NewPoster(pool, rdb)
	for _, tc := range []struct {
		name                  string
		balance, held, amount int64
	}{{"unposted-usage", 600, 0, 700}, {"gateway-hold", 1000, 700, 400}} {
		t.Run(tc.name, func(t *testing.T) {
			setHot(t, rdb, account, "balance", tc.balance, "reserved", tc.held, "ver", 1, "base", 0)
			_, err := poster.Post(ctx, billing.Entry{AccountID: account, Amount: -credits.Micro(tc.amount), Kind: "transfer", OperationID: tc.name})
			if !errors.Is(err, gateway.ErrInsufficientCredits) {
				t.Fatalf("debit admitted or wrong error: %v", err)
			}
			if bal, ver := pgBalance(t, pool, account); bal != 1000 || ver != 0 {
				t.Fatalf("failed debit changed PG: %d v%d", bal, ver)
			}
			if postingHeld(t, rdb, account) != tc.held {
				t.Fatal("failed debit changed holds")
			}
		})
	}
}

func TestCommittedBusinessDebitKeepsHoldUntilIdempotentDelivery(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 7, 1000)
	poster := NewPoster(pool, rdb)
	entry := billing.Entry{AccountID: account, Amount: -400, Kind: "transfer", OperationID: "purchase"}
	if _, err := poster.Post(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if h := hotOf(t, rdb, account); h.balance != 1000 || postingHeld(t, rdb, account) != 400 {
		t.Fatalf("committed debit not held: %+v", h)
	}
	if n, err := billing.SweepPostingHolds(ctx, rdb, NewAccounts(pool), time.Now().Add(time.Hour), 100); err != nil || n != 0 {
		t.Fatalf("committed hold released before delivery: %d %v", n, err)
	}
	snapshot := &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Multiplier: 1}}, Prices: map[string]catalog.Price{"gpt": {Mode: "per_request", PerRequest: 700}}}
	accounts := NewAccounts(pool)
	settler, err := billing.New(rdb, func() *catalog.Snapshot { return snapshot }, accounts, accounts, billing.Config{}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	request := &gateway.Request{ID: "blocked", Model: "gpt", Principal: gateway.Principal{UserID: 7, Group: "default"}}
	if err := settler.Reserve(ctx, request); !errors.Is(err, gateway.ErrInsufficientCredits) {
		t.Fatalf("gateway ignored business hold: %v", err)
	}
	result, err := poster.Post(ctx, entry)
	if err != nil || !result.Duplicate || postingHeld(t, rdb, account) != 400 {
		t.Fatalf("callback retry doubled hold: %+v %v", result, err)
	}
	relay := NewBalanceRelay(pool, rdb, quiet)
	for i := 0; i < 2; i++ {
		if _, err := relay.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if h := hotOf(t, rdb, account); h.balance != 600 || h.ver != 1 || postingHeld(t, rdb, account) != 0 {
		t.Fatalf("delivery doubled debit or leaked hold: %+v", h)
	}
	if n, _ := rdb.ZCard(ctx, redisx.KeyPostingOpen).Result(); n != 0 {
		t.Fatal("delivered business reservation remains indexed")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.ledger_entries`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("entries=%d %v", count, err)
	}
}

func TestBusinessHoldSurvivesOpenTransactionAndReleasesOnRollback(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 7, 1000)
	poster := NewPoster(pool, rdb)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	entry := billing.Entry{AccountID: account, Amount: -400, Kind: "transfer", OperationID: "rollback"}
	if _, err := poster.PostTx(ctx, tx, entry); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(24 * time.Hour)
	if n, err := billing.SweepPostingHolds(ctx, rdb, NewAccounts(pool), deadline, 100); err != nil || n != 0 {
		t.Fatalf("open transaction hold released: %d %v", n, err)
	}
	if postingHeld(t, rdb, account) != 400 {
		t.Fatal("open transaction lost reserved credit")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := billing.SweepPostingHolds(ctx, rdb, NewAccounts(pool), deadline.Add(time.Second), 100); err != nil || n != 1 {
		t.Fatalf("rolled-back hold not released: %d %v", n, err)
	}
	if h := hotOf(t, rdb, account); h.balance != 1000 || postingHeld(t, rdb, account) != 0 {
		t.Fatalf("rollback changed money: %+v", h)
	}
	if n, _ := rdb.XLen(ctx, redisx.StreamBillingEvents).Result(); n != 0 {
		t.Fatal("business rollback generated fictitious gateway usage")
	}
	if _, err := poster.Post(ctx, entry); err != nil {
		t.Fatalf("operation retry after rollback: %v", err)
	}
}

func TestConcurrentGatewayAndBusinessDebitsShareOneBudget(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	account := fundedAccount(t, pool, 7, 1000)
	setHot(t, rdb, account, "balance", 1000, "reserved", 0, "ver", 0, "base", 0)
	accounts := NewAccounts(pool)
	snapshot := &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Multiplier: 1}}, Prices: map[string]catalog.Price{"gpt": {Mode: "per_request", PerRequest: 100}}}
	settler, err := billing.New(rdb, func() *catalog.Snapshot { return snapshot }, accounts, accounts, billing.Config{}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	poster := NewPoster(pool, rdb)
	var admitted atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 30; i++ {
		wg.Go(func() {
			<-start
			id := fmt.Sprintf("concurrent-%d", i)
			var err error
			if i%2 == 0 {
				err = settler.Reserve(ctx, &gateway.Request{ID: id, Model: "gpt", Principal: gateway.Principal{UserID: 7, Group: "default"}})
			} else {
				_, err = poster.Post(ctx, billing.Entry{AccountID: account, Amount: -100, Kind: "transfer", OperationID: id})
			}
			if err == nil {
				admitted.Add(100)
			} else if !errors.Is(err, gateway.ErrInsufficientCredits) {
				t.Errorf("unexpected admission error: %v", err)
			}
		})
	}
	close(start)
	wg.Wait()
	if n := admitted.Load(); n != 1000 || postingHeld(t, rdb, account) != 1000 {
		t.Fatalf("admitted %d; held %d; want exactly 1000", n, postingHeld(t, rdb, account))
	}
	if _, err := NewBalanceRelay(pool, rdb, quiet).Step(ctx); err != nil {
		t.Fatal(err)
	}
	if h := hotOf(t, rdb, account); h.balance != postingHeld(t, rdb, account) {
		t.Fatalf("remaining gateway budget mismatch: %+v held=%d", h, postingHeld(t, rdb, account))
	}
}

func TestBusinessDebitPreservesBigintPrecision(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	const amount int64 = 9_007_199_254_740_993
	account := fundedAccount(t, pool, 7, amount+1)
	if _, err := NewPoster(pool, rdb).Post(ctx, billing.Entry{AccountID: account, Amount: -credits.Micro(amount), Kind: "transfer", OperationID: "bigint"}); err != nil {
		t.Fatal(err)
	}
	if postingHeld(t, rdb, account) != amount {
		t.Fatal("business hold rounded bigint amount")
	}
	if _, err := NewBalanceRelay(pool, rdb, quiet).Step(ctx); err != nil {
		t.Fatal(err)
	}
	if h := hotOf(t, rdb, account); h.balance != 1 || postingHeld(t, rdb, account) != 0 {
		t.Fatalf("business debit rounded bigint money: %+v", h)
	}
}
