//go:build pgintegration

package ledger

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func budgetAccount(t *testing.T, pool *pgxpool.Pool, amount int64) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind,balance) VALUES('api_key',70,'key_budget',$1) RETURNING id`, amount).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func budgetRequest(id string, account int64) *gateway.Request {
	return &gateway.Request{ID: id, Model: "gpt", Principal: gateway.Principal{UserID: 7, KeyID: 70, Group: "default", BudgetLimited: true, BudgetAccountID: account}}
}

func TestKeyBudgetChargesTotalWhileSubscriptionAndWalletSplitFunding(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	wallet, bucket := fundedAccount(t, pool, 7, 1000), fundedAccount(t, pool, 8, 100)
	budget := budgetAccount(t, pool, 250)
	accounts := NewAccounts(pool)
	snapshot := &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Multiplier: 1}}, Prices: map[string]catalog.Price{"gpt": {Mode: "per_request", PerRequest: 200}},
		AccountProfiles: map[int64]catalog.AccountProfile{7: {UserID: 7, WalletAccountID: wallet, Subscriptions: []catalog.SubscriptionBucket{{AccountID: bucket, ExpiresAt: time.Now().Add(time.Hour)}}}}}
	settler, err := billing.New(rdb, func() *catalog.Snapshot { return snapshot }, accounts, accounts, billing.Config{}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	request := budgetRequest("limited", budget)
	if err := settler.Reserve(ctx, request); err != nil {
		t.Fatal(err)
	}
	if postingHeld(t, rdb, bucket) != 100 || postingHeld(t, rdb, wallet) != 100 || postingHeld(t, rdb, budget) != 200 {
		t.Fatal("key budget substituted for payment funding")
	}
	for i := 0; i < 2; i++ {
		if err := settler.Finalize(ctx, request, gateway.Outcome{Terminal: gateway.TerminalCompleted, Charge: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := settler.Reserve(ctx, budgetRequest("exhausted", budget)); !errors.Is(err, gateway.ErrInsufficientCredits) {
		t.Fatalf("key spent user wallet beyond its budget: %v", err)
	}
	drain(t, newWorker(t, pool, rdb, WorkerConfig{}))
	for account, want := range map[int64]int64{bucket: 0, wallet: 900, budget: 50} {
		if got, _ := pgBalance(t, pool, account); got != want || postingHeld(t, rdb, account) != 0 {
			t.Fatalf("account=%d got=%d want=%d", account, got, want)
		}
	}
	var logs int
	var amount int64
	if err := pool.QueryRow(ctx, `SELECT count(*),sum(amount) FROM v3_billing.usage_logs`).Scan(&logs, &amount); err != nil || logs != 1 || amount != 200 {
		t.Fatalf("budget duplicated usage: %d %d %v", logs, amount, err)
	}
}

func TestKeyBudgetRefusalAndReleaseAreAtomicWithWallet(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	wallet, budget := fundedAccount(t, pool, 7, 1000), budgetAccount(t, pool, 150)
	accounts := NewAccounts(pool)
	snapshot := &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Multiplier: 1}}, Prices: map[string]catalog.Price{"gpt": {Mode: "per_request", PerRequest: 200}}}
	settler, err := billing.New(rdb, func() *catalog.Snapshot { return snapshot }, accounts, accounts, billing.Config{}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err := settler.Reserve(ctx, budgetRequest("refused", budget)); !errors.Is(err, gateway.ErrInsufficientCredits) {
		t.Fatalf("insufficient budget admitted: %v", err)
	}
	if postingHeld(t, rdb, wallet) != 0 || postingHeld(t, rdb, budget) != 0 {
		t.Fatal("budget refusal left a wallet hold")
	}
	snapshot.Prices["gpt"] = catalog.Price{Mode: "per_request", PerRequest: 100}
	request := budgetRequest("release", budget)
	if err := settler.Reserve(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := settler.Finalize(ctx, request, gateway.Outcome{Terminal: gateway.TerminalUpstreamErrorBeforeOutput}); err != nil {
		t.Fatal(err)
	}
	if h := hotOf(t, rdb, budget); h.balance != 150 || postingHeld(t, rdb, budget) != 0 || postingHeld(t, rdb, wallet) != 0 {
		t.Fatalf("release consumed key budget: %+v", h)
	}
	missing := budgetRequest("missing", 0)
	if err := settler.Reserve(ctx, missing); !errors.Is(err, gateway.ErrBillingUnavailable) {
		t.Fatalf("limited key without account bypassed budget: %v", err)
	}
}

func TestConcurrentLimitedKeyCannotUseUnlimitedWallet(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	wallet, budget := fundedAccount(t, pool, 7, 10000), budgetAccount(t, pool, 500)
	accounts := NewAccounts(pool)
	snapshot := &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Multiplier: 1}}, Prices: map[string]catalog.Price{"gpt": {Mode: "per_request", PerRequest: 100}}}
	settler, err := billing.New(rdb, func() *catalog.Snapshot { return snapshot }, accounts, accounts, billing.Config{}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err := settler.WarmBalances(ctx, []int64{wallet, budget}); err != nil {
		t.Fatal(err)
	}
	var admitted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Go(func() {
			err := settler.Reserve(ctx, budgetRequest(fmt.Sprintf("key-%d", i), budget))
			if err == nil {
				admitted.Add(1)
			} else if !errors.Is(err, gateway.ErrInsufficientCredits) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := admitted.Load(); n != 5 || postingHeld(t, rdb, budget) != 500 || postingHeld(t, rdb, wallet) != 500 {
		t.Fatalf("limited key admitted %d requests", n)
	}
}
