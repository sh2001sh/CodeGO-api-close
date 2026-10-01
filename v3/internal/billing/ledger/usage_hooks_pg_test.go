//go:build pgintegration

package ledger

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type txUsageRecorder struct{ fail bool }

func (r *txUsageRecorder) RecordUsageTx(ctx context.Context, tx pgx.Tx, _ int64, request string, amount credits.Micro) error {
	_, err := tx.Exec(ctx, `INSERT INTO v3_billing.test_usage_effects VALUES($1,'usage',$2)`, request, amount)
	return err
}

func (r *txUsageRecorder) RecordDiscountUsageTx(ctx context.Context, tx pgx.Tx, _, _, _ int64, request string, before, after credits.Micro) error {
	var found bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_billing.test_usage_effects WHERE request_id=$1 AND step='usage')`, request).Scan(&found); err != nil {
		return err
	}
	if !found {
		return errors.New("discount callback ran before usage callback")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO v3_billing.test_usage_effects VALUES($1,'discount',$2)`, request, before-after); err != nil {
		return err
	}
	if r.fail {
		return errors.New("recorder unavailable after state write")
	}
	return nil
}

func usageHookFixture(t *testing.T) (*pgxpool.Pool, int64) {
	t.Helper()
	pool := testPool(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE v3_billing.test_usage_effects(request_id text NOT NULL,step text NOT NULL,amount bigint NOT NULL,PRIMARY KEY(request_id,step))`); err != nil {
		t.Fatal(err)
	}
	return pool, fundedAccount(t, pool, 7, 2000)
}

func usageHookEvent(t *testing.T, account int64, request string, amount int64, card bool) event {
	t.Helper()
	values := map[string]any{billing.FieldRequestID: request, billing.FieldAccountID: fmt.Sprint(account), billing.FieldAmount: fmt.Sprint(amount),
		billing.FieldUserID: "7", billing.FieldKeyID: "70", billing.FieldModel: "gpt", billing.FieldChannelID: "3", billing.FieldTerminal: "completed"}
	if card {
		values[billing.FieldCardID], values[billing.FieldCardBefore], values[billing.FieldCardAfter] = "81", "1000", fmt.Sprint(amount)
	}
	e, err := parseEvent("1-0", values)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func usageEffects(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.test_usage_effects`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestUsageHooksRollbackMoneyDedupAndDomainWritesTogether(t *testing.T) {
	// Regression: a failed card/progress callback must not leave its write,
	// ledger debit, usage log or dedup claim committed independently.
	pool, account := usageHookFixture(t)
	recorder := &txUsageRecorder{fail: true}
	w, err := NewWorker(pool, nil, WorkerConfig{Consumer: "hooks", Marketplace: recorder}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	e := usageHookEvent(t, account, "rollback", 900, true)
	if _, err := postWithMarketplace(ctx, pool, []event{e}, nil, w.cfg.Marketplace, w.cfg.UsageHook); err == nil {
		t.Fatal("expected callback failure")
	}
	if bal, version := pgBalance(t, pool, account); bal != 2000 || version != 0 || usageEffects(t, pool) != 0 {
		t.Fatalf("failed callback partially committed: balance=%d version=%d", bal, version)
	}
	for _, table := range []string{"billing_dedup", "ledger_entries", "usage_logs"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM v3_billing."+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s contains %d failed writes: %v", table, count, err)
		}
	}
	recorder.fail = false
	res, err := postWithMarketplace(ctx, pool, []event{e}, nil, w.cfg.Marketplace, w.cfg.UsageHook)
	if err != nil || res.posted != 1 {
		t.Fatalf("retry post=%+v err=%v", res, err)
	}
	if bal, version := pgBalance(t, pool, account); bal != 1100 || version != 1 || usageEffects(t, pool) != 2 {
		t.Fatalf("successful callback money=%d version=%d effects=%d", bal, version, usageEffects(t, pool))
	}
	res, err = postWithMarketplace(ctx, pool, []event{e}, nil, w.cfg.Marketplace, w.cfg.UsageHook)
	if err != nil || res.duplicates != 1 || usageEffects(t, pool) != 2 {
		t.Fatalf("replay reapplied callback: %+v %v", res, err)
	}
}

func TestUsageHooksPrimaryAggregateFreeUsageAndLockOrder(t *testing.T) {
	pool, account := usageHookFixture(t)
	other := fundedAccount(t, pool, 8, 2000)
	w, err := NewWorker(pool, nil, WorkerConfig{Consumer: "hooks", Marketplace: &txUsageRecorder{}, UsageHook: func(ctx context.Context, tx pgx.Tx, fields map[string]string) error {
		var accountLocks int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE pid=pg_backend_pid() AND relation='v3_billing.accounts'::regclass AND mode='RowShareLock'`).Scan(&accountLocks); err != nil {
			return err
		}
		if accountLocks != 0 {
			return errors.New("callback invoked after account locks")
		}
		_, err := tx.Exec(ctx, `INSERT INTO v3_billing.test_usage_effects VALUES($1,'generic',0)`, fields[billing.FieldRequestID])
		return err
	}}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	primary := usageHookEvent(t, account, "split", 900, true)
	primary.amount, primary.fields[billing.FieldAmount] = 600, "600"
	primary.fields["funding_part"], primary.fields["usage_total_amount"] = "primary", "900"
	primary.fingerprint = fingerprint(primary.fields)
	secondary := usageHookEvent(t, other, "split", 300, false)
	secondary.fields["funding_part"], secondary.fields["usage_total_amount"] = "secondary", "900"
	secondary.fingerprint = fingerprint(secondary.fields)
	free := usageHookEvent(t, account, "free", 0, false)
	if _, err := postWithMarketplace(ctx, pool, []event{primary, secondary, free}, nil, w.cfg.Marketplace, w.cfg.UsageHook); err != nil {
		t.Fatal(err)
	}
	if usageEffects(t, pool) != 5 {
		t.Fatalf("secondary or free usage callback count incorrect: %d", usageEffects(t, pool))
	}
	var paid, freeAmount int64
	if err := pool.QueryRow(ctx, `SELECT amount FROM v3_billing.test_usage_effects WHERE request_id='split' AND step='usage'`).Scan(&paid); err != nil || paid != 900 {
		t.Fatalf("primary callback amount=%d err=%v", paid, err)
	}
	if err := pool.QueryRow(ctx, `SELECT amount FROM v3_billing.test_usage_effects WHERE request_id='free' AND step='usage'`).Scan(&freeAmount); err != nil || freeAmount != 0 {
		t.Fatalf("free callback amount=%d err=%v", freeAmount, err)
	}
}

func TestConcurrentUsageHookReplayRunsOnce(t *testing.T) {
	pool, account := usageHookFixture(t)
	w, err := NewWorker(pool, nil, WorkerConfig{Consumer: "hooks", Marketplace: &txUsageRecorder{}}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	e := usageHookEvent(t, account, "concurrent", 900, true)
	const count = 20
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := postWithMarketplace(ctx, pool, []event{e}, nil, w.cfg.Marketplace, w.cfg.UsageHook)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if bal, version := pgBalance(t, pool, account); bal != 1100 || version != 1 || usageEffects(t, pool) != 2 {
		t.Fatalf("concurrent replay money=%d version=%d domain writes=%d", bal, version, usageEffects(t, pool))
	}
}
