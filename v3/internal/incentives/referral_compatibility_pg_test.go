//go:build pgintegration

package incentives

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestLegacyReferralCutoverPreservesFrozenPendingPromise(t *testing.T) {
	s, pool, now := fixture(t)
	ctx := context.Background()
	referralOrderFixture(t, s, 100, 2, "subscription", "legacy")
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.orders SET created_at=$1 WHERE id=100`, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	reserveReferral(t, s, 100)
	*now = now.Add(time.Second)
	enableReferral(t, s, 1000000, 2000000)
	*now = now.Add(24 * time.Hour)
	fulfillReferral(t, s, 100, 2, "subscription")
	sum, err := s.ResetOpportunities(ctx, 1)
	if err != nil || sum.EarnedTotal != 1 {
		t.Fatalf("old promise lost=%+v %v", sum, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username,inviter_id) VALUES(4,'new-legacy',1)`); err != nil {
		t.Fatal(err)
	}
	p, err := s.ReferralPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.Enabled = false
	if _, err = s.UpdateReferralPolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	referralOrderFixture(t, s, 101, 4, "subscription", "legacy")
	reserveReferral(t, s, 101)
	fulfillReferral(t, s, 101, 4, "subscription")
	sum, err = s.ResetOpportunities(ctx, 1)
	if err != nil || sum.EarnedTotal != 1 {
		t.Fatalf("new legacy revived resets=%+v %v", sum, err)
	}
}
func TestLegacyOpportunityNeverSelectsStandardV2OrConvertedSubscription(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	seedDraw(t, s, 0)
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET policy_version='standard_v2' WHERE id=1;
 INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,earned_total,available_total) VALUES(1,1,1)`); err != nil {
		t.Fatal(err)
	}
	called := false
	s.cfg.ResetSubscriptionTx = func(context.Context, pgx.Tx, int64, string) error { called = true; return nil }
	if _, err := s.UseReset(ctx, 1); !errors.Is(err, ErrNotFound) || called {
		t.Fatalf("v2 reset=%v callback=%v", err, called)
	}
	sum, err := s.ResetOpportunities(ctx, 1)
	if err != nil || sum.AvailableCount != 1 {
		t.Fatalf("ineligible spent opportunity=%+v %v", sum, err)
	}
}
func TestConvertedLegacyLuckyBenefitDoesNotRestartRetiredDraws(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	seedDraw(t, s, 0)
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET state='canceled',converted_at=$1,benefits_until=expires_at,next_reset_at=NULL,reset_period='never',plan_snapshot='{"plan_type":"monthly","lucky_draw_enabled":true,"membership_tier":"pro"}' WHERE id=1`, s.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Backfill(ctx); !errors.Is(err, ErrRetired) {
		t.Fatalf("retired backfill: %v", err)
	}
	if _, err := s.Self(ctx, 1); !errors.Is(err, ErrRetired) {
		t.Fatalf("converted subscription reopened retired participation: %v", err)
	}
}
