//go:build pgintegration

package ledger

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestFundingLedgerAndUsageLogMatchSplitCharge(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	wallet := fundedAccount(t, pool, 7, 1000)
	bucket := fundedAccount(t, pool, 8, 100)
	accounts := NewAccounts(pool)
	if err := accounts.WarmWallets(ctx); err != nil {
		t.Fatal(err)
	}
	profiles := NewFundingAccounts(accounts, &fundingLoader{sources: []FundingSource{{UserID: 7, AccountID: bucket, ExpiresAt: time.Now().Add(time.Hour)}}}, nil)
	if err := profiles.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot := &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Multiplier: 1}},
		Prices: map[string]catalog.Price{"gpt": {InputPerMTok: 1000000, OutputPerMTok: 2000000}}}
	settler, err := billing.New(rdb, func() *catalog.Snapshot { return snapshot }, profiles, profiles, billing.Config{}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err := settler.WarmBalances(ctx, []int64{wallet, bucket}); err != nil {
		t.Fatal(err)
	}
	before := pool.Stat().AcquireCount()
	request := &gateway.Request{ID: "funded", Model: "gpt", Body: []byte(`{"model":"gpt","max_tokens":100}`),
		Principal: gateway.Principal{UserID: 7, KeyID: 70, Group: "default"}}
	if err := settler.Reserve(ctx, request); err != nil {
		t.Fatal(err)
	}
	out := gateway.Outcome{Terminal: gateway.TerminalCompleted, Charge: true, Usage: gateway.Usage{PromptTokens: 10, CompletionTokens: 120}}
	if err := settler.Finalize(ctx, request, out); err != nil {
		t.Fatal(err)
	}
	if after := pool.Stat().AcquireCount(); after != before {
		t.Fatalf("warm funding queried PG: %d acquisitions", after-before)
	}
	drain(t, newWorker(t, pool, rdb, WorkerConfig{}))
	if bal, ver := pgBalance(t, pool, wallet); bal != 850 || ver != 1 {
		t.Fatalf("wallet=%d v%d", bal, ver)
	}
	if bal, ver := pgBalance(t, pool, bucket); bal != 0 || ver != 1 {
		t.Fatalf("bucket=%d v%d", bal, ver)
	}
	var count int
	var amount, prompt, completion int64
	if err := pool.QueryRow(ctx, `SELECT count(*),sum(amount),sum(prompt_tokens),sum(completion_tokens) FROM v3_billing.usage_logs`).
		Scan(&count, &amount, &prompt, &completion); err != nil {
		t.Fatal(err)
	}
	if count != 1 || amount != 250 || prompt != 10 || completion != 120 {
		t.Fatalf("request logged twice or partial: n=%d amount=%d tokens=%d/%d", count, amount, prompt, completion)
	}
	if _, err := NewReconciler(pool, rdb, quiet).Run(ctx); err != nil {
		t.Fatal(err)
	}
}
