//go:build pgintegration

package commerce_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func rewardMonthlyPlan(t *testing.T, s *commerce.Service, pool *pgxpool.Pool, tier string, price, amount, period int64) commerce.Plan {
	t.Helper()
	p, err := s.SavePlan(context.Background(), commerce.Plan{Name: tier, PriceMinor: price, Currency: "usd", Credits: credits.Micro(amount),
		PeriodCredits: credits.Micro(period), PlanType: "monthly", DurationUnit: "month", DurationValue: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(context.Background(), `UPDATE v3_commerce.plans SET membership_tier=$2 WHERE id=$1`, p.ID, tier); err != nil {
		t.Fatal(err)
	}
	return p
}

func rewardMarket(s *commerce.Service, pool *pgxpool.Pool, now *time.Time) *marketplace.Service {
	return marketplace.New(pool, ledger.NewPoster(pool), nil, s, s, marketplace.Config{Now: func() time.Time { return *now }})
}

func TestRewardSubscriptionMergesPreservingTierExpiryUsageAndConcurrentReceipt(t *testing.T) {
	s, pool, now := newService(t)
	s.SetMonthlyBenefits(rewardMarket(s, pool, now))
	ctx := context.Background()
	p := rewardMonthlyPlan(t, s, pool, "ultra", 1000, 1000, 500)
	o := create(t, s, p.ID)
	body, err := json.Marshal(payment(o))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleWebhook(ctx, "test", http.Header{}, body); err != nil {
		t.Fatal(err)
	}
	subs, err := s.ListSubscriptions(ctx, 1)
	if err != nil || len(subs) != 1 {
		t.Fatalf("initial subscription %v err=%v", subs, err)
	}
	base := subs[0]
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET used_credits=70,period_used=70 WHERE id=$1`, base.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: base.AccountID, Amount: -30, Kind: "usage", OperationID: "reward:test:spent"}); err != nil {
		t.Fatal(err)
	}
	reward := rewardMonthlyPlan(t, s, pool, "lite", 100, 200, 0)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.GrantRewardTx(ctx, tx, 1, reward.ID, "blind-box:prop:11") })
		}()
	}
	wg.Wait()
	close(results)
	for result := range results {
		if result != nil {
			t.Fatal(result)
		}
	}
	subs, err = s.ListSubscriptions(ctx, 1)
	if err != nil || len(subs) != 1 {
		t.Fatalf("reward created extra package: %v err=%v", subs, err)
	}
	got := subs[0]
	if got.PlanID != p.ID || got.AccountID != base.AccountID || got.TotalCredits != 1200 || got.PeriodCredits != 700 || got.UsedCredits != 100 || got.PeriodUsed != 100 || got.Balance != 670 || !got.ExpiresAt.Equal(base.ExpiresAt) {
		t.Fatalf("merged package=%+v base=%+v", got, base)
	}
	var receipts, grants, seconds int64
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.subscription_reward_receipts),
	 (SELECT count(*) FROM v3_billing.ledger_entries WHERE operation_id='subscription:reward:blind-box:prop:11'),
	 COALESCE((SELECT remaining_seconds FROM v3_marketplace.blind_box_props WHERE user_id=1 AND prop_type='monthly_pass_multiplier'),0)`).Scan(&receipts, &grants, &seconds); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 || grants != 1 || seconds != 0 {
		t.Fatalf("receipts=%d grants=%d card seconds=%d", receipts, grants, seconds)
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.GrantRewardTx(ctx, tx, 2, reward.ID, "blind-box:prop:11") })
	if !errors.Is(err, billing.ErrPostConflict) {
		t.Fatalf("foreign replay=%v", err)
	}
}

type failedMonthlyBenefit struct {
	market *marketplace.Service
	err    error
}

func (f failedMonthlyBenefit) GrantMonthlyCardTx(ctx context.Context, tx pgx.Tx, user, seconds int64, ref string) error {
	if err := f.market.GrantMonthlyCardTx(ctx, tx, user, seconds, ref); err != nil {
		return err
	}
	return f.err
}

func TestRewardSubscriptionRetainedMonthlyFailureRollsBackPaymentAndNewRewardsSkipCards(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	market := rewardMarket(s, pool, now)
	sentinel := errors.New("monthly injected failure")
	s.SetMonthlyBenefits(failedMonthlyBenefit{market, sentinel})
	p := rewardMonthlyPlan(t, s, pool, "standard", 1000, 1000, 0)
	o := create(t, s, p.ID)
	retainMonthlySnapshot(t, pool, o.ID, 1800, 0)
	if err := s.Fulfill(ctx, "test", payment(o)); !errors.Is(err, sentinel) {
		t.Fatalf("payment callback error=%v", err)
	}
	var subCount, ledgerCount, eventCount, propCount int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.subscriptions),(SELECT count(*) FROM v3_billing.ledger_entries),
	 (SELECT count(*) FROM v3_commerce.payment_events),(SELECT count(*) FROM v3_marketplace.blind_box_props)`).Scan(&subCount, &ledgerCount, &eventCount, &propCount); err != nil {
		t.Fatal(err)
	}
	if subCount+ledgerCount+eventCount+propCount != 0 {
		t.Fatalf("partial payment committed: %d %d %d %d", subCount, ledgerCount, eventCount, propCount)
	}
	s.SetMonthlyBenefits(market)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	s.SetMonthlyBenefits(failedMonthlyBenefit{market, sentinel})
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.GrantRewardTx(ctx, tx, 1, p.ID, "failure-reward") })
	if err != nil {
		t.Fatalf("reward callback error=%v", err)
	}
	var total, balance, rewards, seconds int64
	if err = pool.QueryRow(ctx, `SELECT s.total_credits,a.balance,(SELECT count(*) FROM v3_commerce.subscription_reward_receipts),
	 (SELECT remaining_seconds FROM v3_marketplace.blind_box_props WHERE user_id=1 AND prop_type='monthly_pass_multiplier')
	 FROM v3_commerce.subscriptions s JOIN v3_billing.accounts a ON a.id=s.account_id WHERE s.user_id=1`).Scan(&total, &balance, &rewards, &seconds); err != nil {
		t.Fatal(err)
	}
	if total != 2000 || balance != 2000 || rewards != 1 || seconds != 1800 {
		t.Fatalf("partial reward committed: %d %d %d %d", total, balance, rewards, seconds)
	}
}
