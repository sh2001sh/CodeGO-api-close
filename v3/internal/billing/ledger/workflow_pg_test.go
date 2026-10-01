//go:build pgintegration

package ledger

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestWorkflowDurableFundingPostsExactlyOnce(t *testing.T) {
	pool, rdb := testPool(t), testRedis(t)
	wallet, bucket := fundedAccount(t, pool, 7, 1000), fundedAccount(t, pool, 8, 100)
	budget := budgetAccount(t, pool, 500)
	accounts := NewAccounts(pool)
	snapshot := &catalog.Snapshot{Groups: map[string]catalog.Group{"default": {Multiplier: 1}},
		Prices:          map[string]catalog.Price{"gpt": {InputPerMTok: 1000000, OutputPerMTok: 2000000}},
		AccountProfiles: map[int64]catalog.AccountProfile{7: {WalletAccountID: wallet, Subscriptions: []catalog.SubscriptionBucket{{AccountID: bucket, ExpiresAt: time.Now().Add(time.Hour)}}}}}
	settler, err := billing.New(rdb, func() *catalog.Snapshot { return snapshot }, accounts, accounts, billing.Config{}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	request := &gateway.Request{ID: "durable-charge", Model: "gpt", Received: time.Now(), Body: []byte(`{"max_tokens":100}`),
		Principal: gateway.Principal{UserID: 7, KeyID: 70, Group: "default", BudgetLimited: true, BudgetAccountID: budget},
		Targets:   []gateway.Target{{ChannelID: 3, CredentialID: 300, Provider: "openai_video", UpstreamModel: "gpt"}}}
	reservation, err := billing.NewWorkflowSettler(settler).Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	repository := &workflow.PostgresRepository{Pool: pool}
	task := workflow.Task{ID: request.ID, UserID: 7, KeyID: 70, Group: "default", Model: "gpt", Body: request.Body,
		Provider: "openai_video", ChannelID: 3, CredentialID: 300, UpstreamModel: "gpt", UpstreamID: "upstream",
		Action: "generate", Status: "submitting", CostState: "reserved", Reservation: reservation, CreatedAt: request.Received}
	if err := repository.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_workflow.tasks SET status='completed',upstream_id='upstream' WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	persisted, err := repository.GetOwned(ctx, request.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	result := native.Result{Status: "completed", Usage: gateway.Usage{PromptTokens: 10, CompletionTokens: 120}}
	for range 2 {
		actual, err := billing.NewWorkflowSettler(settler).Finalize(ctx, persisted.Request(), persisted.Reservation, result)
		if err != nil || actual != 250 {
			t.Fatalf("actual=%d err=%v", actual, err)
		}
	}
	drain(t, newWorker(t, pool, rdb, WorkerConfig{}))
	for account, want := range map[int64]int64{wallet: 850, bucket: 0, budget: 250} {
		if got, _ := pgBalance(t, pool, account); got != want {
			t.Fatalf("account=%d got=%d want=%d", account, got, want)
		}
	}
	var logs, debits int
	var amount int64
	if err := pool.QueryRow(ctx, `SELECT count(*),sum(amount) FROM v3_billing.usage_logs`).Scan(&logs, &amount); err != nil || logs != 1 || amount != 250 {
		t.Fatalf("usage logs=%d amount=%d err=%v", logs, amount, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.ledger_entries WHERE request_id=$1`, request.ID).Scan(&debits); err != nil || debits != 3 {
		t.Fatalf("ledger debits=%d err=%v", debits, err)
	}
}

func TestWorkflowTaskFactsRetainEveryDurableState(t *testing.T) {
	pool := testPool(t)
	accounts := NewAccounts(pool)
	for _, state := range []string{"submitting", "submission_unknown", "queued", "in_progress", "completed", "failed"} {
		t.Run(state, func(t *testing.T) {
			repository := &workflow.PostgresRepository{Pool: pool}
			task := workflow.Task{ID: "facts-" + state, UserID: 7, KeyID: 70, Group: "default", Provider: "p", ChannelID: 3, CredentialID: 300,
				Model: "m", UpstreamModel: "m", UpstreamID: "upstream", Action: "generate", Status: "submitting", Body: []byte(`{}`), CreatedAt: time.Now()}
			if err := repository.Create(ctx, task); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE v3_workflow.tasks SET status=$2,upstream_id='upstream' WHERE id=$1`, task.ID, state); err != nil {
				t.Fatal(err)
			}
			found, err := accounts.AsyncTaskExists(ctx, task.ID, 7, 70)
			if err != nil || !found {
				t.Fatalf("state=%s found=%v err=%v", state, found, err)
			}
			if _, err := accounts.AsyncTaskExists(ctx, task.ID, 8, 70); err == nil {
				t.Fatal("cross-user task accepted")
			}
		})
	}
	if found, err := accounts.AsyncTaskExists(ctx, "missing", 7, 70); err != nil || found {
		t.Fatalf("absence=%v err=%v", found, err)
	}
}
