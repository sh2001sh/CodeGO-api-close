//go:build pgintegration

package ledger

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestSubscriptionModelUsagePersistsActualSecondaryDebitsAndRollsBack(t *testing.T) {
	pool := testPool(t)
	_, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(7,'model-usage');
	 INSERT INTO v3_commerce.plans(id,name,price_minor,credits,period_seconds) VALUES(1,'model caps',100,1000,3600);
	 INSERT INTO v3_billing.accounts(id,owner_type,owner_id,kind,balance) OVERRIDING SYSTEM VALUE VALUES
	 (43,'subscription',1,'subscription',1000),(44,'api_key',70,'key_budget',1000);
	 INSERT INTO v3_commerce.subscriptions(id,user_id,plan_id,account_id,starts_at,expires_at,model_usage) VALUES
	 (1,7,1,43,now(),now()+interval '1 hour','{"gpt":9}')`)
	if err != nil {
		t.Fatal(err)
	}
	first := usageHookEvent(t, 43, "model-request-1", 100, false)
	second := usageHookEvent(t, 43, "model-request-2", 200, false)
	second.fields["funding_part"] = "secondary"
	second.fingerprint = fingerprint(second.fields)
	budget := usageHookEvent(t, 44, "model-request-1", 100, false)
	budget.fields["funding_part"] = "secondary"
	budget.fingerprint = fingerprint(budget.fields)
	batch := []event{first, second, budget}
	fail := func(context.Context, pgx.Tx, map[string]string) error {
		return errors.New("usage side effect unavailable")
	}
	if _, err = post(ctx, pool, batch, nil, fail); err == nil {
		t.Fatal("failed usage hook committed model usage")
	}
	var used int64
	if err = pool.QueryRow(ctx, `SELECT (model_usage->>'gpt')::bigint FROM v3_commerce.subscriptions WHERE id=1`).Scan(&used); err != nil || used != 9 {
		t.Fatalf("rolled-back model usage=%d %v", used, err)
	}
	for range 2 {
		if _, err = post(ctx, pool, batch, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = pool.QueryRow(ctx, `SELECT (model_usage->>'gpt')::bigint FROM v3_commerce.subscriptions WHERE id=1`).Scan(&used); err != nil || used != 309 {
		t.Fatalf("actual sub usage=%d %v; want309 (no budget mirror)", used, err)
	}
	if balance, version := pgBalance(t, pool, 43); balance != 700 || version != 2 {
		t.Fatalf("subscription money=%d/%d", balance, version)
	}
}

func TestSubscriptionModelUsageOverflowDoesNotAcceptLedger(t *testing.T) {
	pool := testPool(t)
	_, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(7,'overflow-usage');
	 INSERT INTO v3_commerce.plans(id,name,price_minor,credits,period_seconds) VALUES(1,'model caps',100,1000,3600);
	 INSERT INTO v3_billing.accounts(id,owner_type,owner_id,kind,balance) OVERRIDING SYSTEM VALUE VALUES(43,'subscription',1,'subscription',1000);
	 INSERT INTO v3_commerce.subscriptions(id,user_id,plan_id,account_id,starts_at,expires_at,model_usage) VALUES
	 (1,7,1,43,now(),now()+interval '1 hour','{"gpt":9223372036854775807}')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = post(ctx, pool, []event{usageHookEvent(t, 43, "overflow-model", 1, false)}, nil); err == nil {
		t.Fatal("model usage overflow accepted")
	}
	if balance, version := pgBalance(t, pool, 43); balance != 1000 || version != 0 {
		t.Fatalf("overflow consumed money=%d/%d", balance, version)
	}
	if count(t, pool, "billing_dedup") != 0 || count(t, pool, "ledger_entries") != 0 {
		t.Fatal("overflow left dedup or ledger artifacts")
	}
}
