//go:build pgintegration

package ledger

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type notifiedUsageRecorder struct {
	wrapped UsageRecorder
	started chan struct{}
}

func (r *notifiedUsageRecorder) RecordUsageTx(ctx context.Context, tx pgx.Tx, user int64, request string, amount credits.Micro) error {
	close(r.started)
	return r.wrapped.RecordUsageTx(ctx, tx, user, request, amount)
}
func (r *notifiedUsageRecorder) RecordDiscountUsageTx(ctx context.Context, tx pgx.Tx, user, prop, channel int64, request string, before, after credits.Micro) error {
	return r.wrapped.RecordDiscountUsageTx(ctx, tx, user, prop, channel, request, before, after)
}

func TestWorkerMarketplaceSubscriptionAndAccountLocksMatchRewardGrant(t *testing.T) {
	pool := testPool(t)
	_, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(7,'grant-locks');
	 INSERT INTO v3_commerce.plans(id,name,price_minor,credits,period_seconds) VALUES(1,'model caps',100,1000,3600);
	 INSERT INTO v3_billing.accounts(id,owner_type,owner_id,kind,balance) OVERRIDING SYSTEM VALUE VALUES(43,'subscription',1,'subscription',1000);
	 INSERT INTO v3_commerce.subscriptions(id,user_id,plan_id,account_id,starts_at,expires_at) VALUES(1,7,1,43,now(),now()+interval '1 hour');
	 INSERT INTO v3_marketplace.blind_box_zero_hour_states(user_id) VALUES(7)`)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = grant.Rollback(ctx) }()
	if _, err = grant.Exec(ctx, `SELECT user_id FROM v3_marketplace.blind_box_zero_hour_states WHERE user_id=7 FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	recorder := &notifiedUsageRecorder{wrapped: marketplace.New(pool, nil, nil, nil, nil, marketplace.Config{}), started: make(chan struct{})}
	ledgerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	accepted := usageHookEvent(t, 43, "grant-lock-order", 100, false)
	go func() {
		_, err := postWithMarketplace(ledgerCtx, pool, []event{accepted}, nil, recorder,
			func(ctx context.Context, tx pgx.Tx, fields map[string]string) error {
				var used, accountLocks int64
				if err := tx.QueryRow(ctx, `SELECT (model_usage->>'gpt')::bigint FROM v3_commerce.subscriptions WHERE id=1`).Scan(&used); err != nil {
					return err
				}
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE pid=pg_backend_pid() AND relation='v3_billing.accounts'::regclass AND mode='RowShareLock'`).Scan(&accountLocks); err != nil {
					return err
				}
				if used != 100 || accountLocks != 0 || fields[billing.FieldRequestID] != "grant-lock-order" {
					return fmt.Errorf("income phase before model state or after account locks: %d/%d", used, accountLocks)
				}
				return nil
			})
		result <- err
	}()
	select {
	case <-recorder.started:
	case <-ledgerCtx.Done():
		t.Fatal("worker never reached actual marketplace callback")
	}
	// The worker now waits for grant's marketplace state. Grant must still be
	// able to lock its subscription; reverse worker order would form a cycle.
	grantCtx, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	if _, err = grant.Exec(grantCtx, `SELECT id FROM v3_commerce.subscriptions WHERE id=1 FOR UPDATE`); err != nil {
		t.Fatalf("worker acquired subscription ahead of marketplace: %v", err)
	}
	if err = grant.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-result; err != nil {
		t.Fatal(err)
	}
	if balance, version := pgBalance(t, pool, 43); balance != 900 || version != 1 {
		t.Fatalf("accepted usage=%d/%d", balance, version)
	}
}
