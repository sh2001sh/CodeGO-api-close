//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestRewardSubscriptionNewCalendarPackageAndFrozenMonthlyTier(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	s.SetMonthlyBenefits(rewardMarket(s, pool, now))
	p := rewardMonthlyPlan(t, s, pool, "lite", 1000, 1000, 0)
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.GrantRewardTx(ctx, tx, 1, p.ID, "fresh-month-card") })
	if err != nil {
		t.Fatal(err)
	}
	subs, err := s.ListSubscriptions(ctx, 1)
	if err != nil || len(subs) != 1 || !subs[0].ExpiresAt.Equal(now.AddDate(0, 1, 0)) {
		t.Fatalf("calendar reward=%+v err=%v", subs, err)
	}
	// User 2 has no package, so this exercises ordinary paid subscription
	// benefits and edits the plan only after checkout has frozen its rules.
	o, err := s.Create(ctx, commerce.CreateOrder{UserID: 2, PlanID: p.ID, Provider: "test", SuccessURL: "https://site.test/success", CancelURL: "https://site.test/cancel"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.plans SET membership_tier='ultra',plan_type='daily',credits=9000,duration_value=2 WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	var seconds, balance int64
	if err = pool.QueryRow(ctx, `SELECT remaining_seconds,(SELECT a.balance FROM v3_billing.accounts a JOIN v3_commerce.subscriptions s ON a.id=s.account_id WHERE s.user_id=2)
	 FROM v3_marketplace.blind_box_props WHERE user_id=2 AND prop_type='monthly_pass_multiplier'`).Scan(&seconds, &balance); err != nil {
		t.Fatal(err)
	}
	if seconds != 900 || balance != 1000 {
		t.Fatalf("checkout benefit changed after edit seconds=%d balance=%d", seconds, balance)
	}
}

func TestRewardSubscriptionBlocksPendingChangesAndOverflowWithoutPartialGrant(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := rewardMonthlyPlan(t, s, pool, "standard", 1000, 1000, 0)
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	subs, err := s.ListSubscriptions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	id := subs[0].ID
	if _, err = pool.Exec(ctx, `INSERT INTO v3_commerce.subscription_operations(operation_id,subscription_id,actor_id,kind) VALUES('pending-admin',$1,1,'reset')`, id); err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.GrantRewardTx(ctx, tx, 1, p.ID, "pending-reward") })
	if !errors.Is(err, commerce.ErrFundingPending) {
		t.Fatalf("pending reset accepted reward: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.subscription_operations SET state='completed' WHERE operation_id='pending-admin'`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET total_credits=9223372036854775807 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.GrantRewardTx(ctx, tx, 1, p.ID, "overflow-reward") })
	if !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("overflow reward error=%v", err)
	}
	var receipts, balance int64
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.subscription_reward_receipts),balance FROM v3_billing.accounts WHERE id=$1`, subs[0].AccountID).Scan(&receipts, &balance); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 || balance != 1000 {
		t.Fatalf("failed reward partially committed receipts=%d balance=%d", receipts, balance)
	}
}
