//go:build pgintegration

package ledger

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

// Exercise the fresh-event join through the same posting transaction, including
// a secondary subscription debit and a primary wallet event/key-budget mirror.
func TestFundingAuditSubscriptionJoinIsAtomicAndReplaySafe(t *testing.T) {
	pool := testPool(t)
	userWallet, _ := rewardAccount(t, pool, time.Now())
	var plan, subscriptionAccount, key int64
	if err := pool.QueryRow(ctx, `INSERT INTO v3_commerce.plans(name,price_minor,credits,period_seconds) VALUES('model-cap',100,1000,3600) RETURNING id`).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind,balance) VALUES('subscription',91,'subscription',1000) RETURNING id`).Scan(&subscriptionAccount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind,balance) VALUES('api_key',1,'key_budget',1000) RETURNING id`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.subscriptions(id,user_id,plan_id,account_id,starts_at,expires_at,model_usage)
	 VALUES(91,7,$1,$2,now(),now()+interval '1 hour','{"gpt":50}')`, plan, subscriptionAccount); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.accounts SET balance=1000 WHERE id=$1`, userWallet); err != nil {
		t.Fatal(err)
	}
	primary := usageHookEvent(t, userWallet, "subscription-join", 40, false)
	primary.fields["funding_part"], primary.fields["usage_total_amount"] = "primary", "100"
	primary.fields[billing.FieldBillingSource] = "mixed"
	primary.fingerprint = fingerprint(primary.fields)
	secondary := usageHookEvent(t, subscriptionAccount, "subscription-join", 60, false)
	secondary.fields["funding_part"] = "secondary"
	secondary.fingerprint = fingerprint(secondary.fields)
	mirror := usageHookEvent(t, key, "subscription-join", 100, false)
	mirror.fields["funding_part"] = "secondary"
	mirror.fingerprint = fingerprint(mirror.fields)
	batch := []event{primary, secondary, mirror}
	readUsage := func() int64 {
		t.Helper()
		var got int64
		if err := pool.QueryRow(ctx, `SELECT (model_usage->>'gpt')::bigint FROM v3_commerce.subscriptions WHERE id=91`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	broken := func(context.Context, pgx.Tx, map[string]string) error { return errors.New("domain callback rejected") }
	if _, err := post(ctx, pool, batch, nil, broken); err == nil {
		t.Fatal("callback failure lost")
	}
	if got := readUsage(); got != 50 || count(t, pool, "billing_dedup") != 0 {
		t.Fatal("model usage escaped rolled-back ledger")
	}
	for range 2 {
		if _, err := post(ctx, pool, batch, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := readUsage(); got != 110 {
		t.Fatalf("secondary subscription charge/replay/mirror usage=%d want110", got)
	}
	if bal, _ := pgBalance(t, pool, subscriptionAccount); bal != 940 {
		t.Fatalf("subscription balance=%d", bal)
	}
}
