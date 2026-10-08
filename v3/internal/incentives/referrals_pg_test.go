//go:build pgintegration

package incentives

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func seedPurchase(t *testing.T, s *Service, id, plan int64) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(), `INSERT INTO v3_commerce.orders(id,user_id,plan_id,amount_minor,credits,period_seconds,currency,kind,provider,trade_no,state,expires_at,created_at) VALUES($1,2,$2,100,1000000,2592000,'usd','subscription','test',$3,'paid',$4,$5)`, id, plan, fmt.Sprintf("paid-%06d", id), s.now().Add(time.Hour), s.now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
}
func TestFirstMonthlyPurchaseEarnsOneOpportunityAcrossConcurrentReplay(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	seedPurchase(t, s, 1, 2)
	seedPurchase(t, s, 2, 1)
	p := Purchase{UserID: 2, OrderID: 2, PlanID: 1, AmountMinor: 100, SourceType: "subscription_order", SourceID: "paid-000002"}
	bad := p
	bad.SourceID = "other-order"
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.PurchaseTx(ctx, tx, bad) }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("forged origin accepted: %v", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.PurchaseTx(ctx, tx, p) })
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	sum, err := s.ResetOpportunities(ctx, 1)
	if err != nil || sum.EarnedTotal != 1 || sum.AvailableCount != 1 || sum.UsedTotal != 0 {
		t.Fatalf("summary=%+v err=%v", sum, err)
	}
	seedPurchase(t, s, 3, 1)
	p.OrderID = 3
	p.SourceID = "paid-000003"
	if err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.PurchaseTx(ctx, tx, p) }); err != nil {
		t.Fatal(err)
	}
	var reset, money int64
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.subscription_reset_opportunity_ledgers),(SELECT COALESCE(sum(bonus_credits),0) FROM v3_commerce.referral_purchase_rewards)`).Scan(&reset, &money); err != nil || reset != 1 || money != 0 {
		t.Fatalf("reset=%d invented money=%d err=%v", reset, money, err)
	}
	overview, err := s.AffiliateRewards(ctx, 1)
	if err != nil || overview["successful_purchase_invites"] != 1 {
		t.Fatalf("overview=%+v err=%v", overview, err)
	}
}
func TestReferralOverflowRollsBackState(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	seedPurchase(t, s, 1, 1)
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,earned_total,available_total) VALUES(1,$1,$1)`, int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	p := Purchase{UserID: 2, OrderID: 1, PlanID: 1, AmountMinor: 100, SourceType: "subscription_order", SourceID: "paid-000001"}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.PurchaseTx(ctx, tx, p) })
	if !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("overflow accepted: %v", err)
	}
	var n int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscription_reset_opportunity_ledgers`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("overflow leaked ledger %d: %v", n, err)
	}
}
func TestResetCounterTransactionRollbackMonthlyLimitAndNextMonth(t *testing.T) {
	s, pool, now := fixture(t)
	ctx := context.Background()
	seedDraw(t, s, 0)
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET used_credits=650000,period_used=650000 WHERE id=1;INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,earned_total,available_total) VALUES(1,2,2)`); err != nil {
		t.Fatal(err)
	}
	s.cfg.ResetSubscriptionTx = func(ctx context.Context, tx pgx.Tx, id int64, _ string) error {
		if _, err := tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET used_credits=200000,period_used=0 WHERE id=$1`, id); err != nil {
			return err
		}
		return errors.New("drain incomplete")
	}
	if _, err := s.UseReset(ctx, 1); err == nil {
		t.Fatal("failed reset reported success")
	}
	sum, err := s.ResetOpportunities(ctx, 1)
	if err != nil || sum.AvailableCount != 2 || sum.UsedTotal != 0 {
		t.Fatalf("failure spent opportunity %+v: %v", sum, err)
	}
	s.cfg.ResetSubscriptionTx = func(ctx context.Context, tx pgx.Tx, id int64, _ string) error {
		_, err := tx.Exec(ctx, `UPDATE v3_commerce.subscriptions SET used_credits=200000,period_used=0 WHERE id=$1`, id)
		return err
	}
	got, err := s.UseReset(ctx, 1)
	if err != nil || got.ClearedUsed != 450000 || got.ResetOpportunity.AvailableCount != 1 || !got.ResetOpportunity.UsedThisMonth {
		t.Fatalf("reset=%+v: %v", got, err)
	}
	if _, err = s.UseReset(ctx, 1); !errors.Is(err, ErrMonthlyUsed) {
		t.Fatalf("monthly double use accepted: %v", err)
	}
	*now = now.Add(time.Hour * 13)
	got, err = s.UseReset(ctx, 1)
	if err != nil || got.ResetOpportunity.CurrentMonth != "2026-10" || got.ResetOpportunity.AvailableCount != 0 {
		t.Fatalf("month boundary %+v: %v", got, err)
	}
}
