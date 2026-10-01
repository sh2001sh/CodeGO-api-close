//go:build pgintegration

package commerce_test

import (
	"context"
	"math"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestNewGroupBonusSurvivesManualResetAndReplay(t *testing.T) {
	f := newGroupFixture(t)
	p := f.plan(t, 2)
	ctx := context.Background()
	for _, user := range []int64{1, 2} {
		order, status, body := f.checkout(user, map[string]any{"plan_id": p.ID}, false)
		if status != 200 {
			t.Fatalf("checkout %d: %s", status, body)
		}
		if err := f.pay(order); err != nil {
			t.Fatal(err)
		}
		if err := f.pay(order); err != nil {
			t.Fatal(err)
		}
	}
	sub := onlySubscription(t, f.s)
	var renewable int64
	if err := f.pool.QueryRow(ctx, `SELECT renewable_credits FROM v3_commerce.subscriptions WHERE id=$1`, sub.ID).Scan(&renewable); err != nil {
		t.Fatal(err)
	}
	if sub.Balance != 1100 || renewable != 1100 {
		t.Fatalf("group allowance balance=%d renewable=%d, want 1100 each", sub.Balance, renewable)
	}
	if _, err := ledger.NewPoster(f.pool).Post(ctx, billing.Entry{AccountID: sub.AccountID, Amount: -1100, Kind: "usage", OperationID: "group-renewable:spend"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := f.s.ResetSubscription(ctx, sub.ID, 2, "group-renewable:reset"); err != nil {
			t.Fatal(err)
		}
	}
	after := onlySubscription(t, f.s)
	if after.Balance != 1100 || after.TotalCredits != 1100 || after.UsedCredits != 0 || after.AccountID == sub.AccountID {
		t.Fatalf("reset lost paid group allowance: %+v", after)
	}
	var resets int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_operations WHERE subscription_id=$1 AND kind='reset'`, sub.ID).Scan(&resets); err != nil || resets != 1 {
		t.Fatalf("reset replay count=%d error=%v", resets, err)
	}
}

func TestNewGroupRenewableOverflowLeavesBudgetsUnchanged(t *testing.T) {
	f := newGroupFixture(t)
	p := f.plan(t, 2)
	ctx := context.Background()
	if _, err := f.s.BindSubscription(ctx, 1, p.ID, "group-renewable:overflow-sub"); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, f.s)
	if _, err := f.pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET renewable_credits=$2 WHERE id=$1`, sub.ID, int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	err := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		_, err := f.s.PrepareGroupBonusTx(ctx, tx, 1, sub.ID, credits.Micro(1))
		return err
	})
	if err == nil {
		t.Fatal("renewable overflow accepted")
	}
	var total, period, renewable int64
	if err := f.pool.QueryRow(ctx, `SELECT total_credits,period_credits,renewable_credits FROM v3_commerce.subscriptions WHERE id=$1`, sub.ID).Scan(&total, &period, &renewable); err != nil {
		t.Fatal(err)
	}
	if total != int64(sub.TotalCredits) || period != int64(sub.PeriodCredits) || renewable != math.MaxInt64 {
		t.Fatalf("overflow mutated budgets %d/%d/%d", total, period, renewable)
	}
}
