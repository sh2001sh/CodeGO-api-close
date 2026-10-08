//go:build pgintegration

package incentives

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestV2ReferralMaturityFrozenProfitBudgetReplayAndChargeback(t *testing.T) {
	s, pool, now := fixture(t)
	ctx := context.Background()
	enableReferral(t, s, 2000000, 10000000)
	referralOrderFixture(t, s, 100, 2, "subscription", "standard_v2")
	reserveReferral(t, s, 100)
	fulfillReferral(t, s, 100, 2, "subscription")
	sum, err := s.ResetOpportunities(ctx, 1)
	if err != nil || sum.EarnedTotal != 0 {
		t.Fatalf("new package minted reset: %+v %v", sum, err)
	}
	subscriptionReferralFact(t, s, 100, 10000000, 9000000, "v2-first", true)
	if _, err = s.SettleReferrals(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if b := referralWalletBalance(t, s, 1); b != 0 {
		t.Fatalf("immature reward=%d", b)
	}
	*now = now.Add(8 * 24 * time.Hour)
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.SettleReferrals(ctx, 100); failures <- e }()
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	if b := referralWalletBalance(t, s, 1); b != 100000 {
		t.Fatalf("replayed reward=%d want100000", b)
	}
	p, err := s.ReferralPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.RewardPPM = 5000
	if _, err = s.UpdateReferralPolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	subscriptionReferralFact(t, s, 100, 20000000, 19990000, "v2-profit-cap", true)
	*now = now.Add(8 * 24 * time.Hour)
	if _, err = s.SettleReferrals(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if b := referralWalletBalance(t, s, 1); b != 202000 {
		t.Fatalf("frozen1%%/profit cap reward=%d", b)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.orders SET state='refunded' WHERE id=100`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = s.SettleReferrals(ctx, 100); err != nil {
			t.Fatal(err)
		}
	}
	if b := referralWalletBalance(t, s, 1); b != 202000 {
		t.Fatalf("chargeback debited paid wallet=%d", b)
	}
	var debt int64
	if err = pool.QueryRow(ctx, `SELECT debt_credits FROM v3_commerce.referral_consumption_offsets WHERE user_id=1`).Scan(&debt); err != nil || debt != 202000 {
		t.Fatalf("debt=%d err=%v", debt, err)
	}
	p, err = s.ReferralPolicy(ctx)
	if err != nil || p.ReservedCredits != 0 || p.SpentCredits != 202000 {
		t.Fatalf("budget=%+v err=%v", p, err)
	}
	var knownZero, locked bool
	if err = pool.QueryRow(ctx, `SELECT bool_and(revenue_multiplier_ppm=0),bool_and(non_transferable AND non_refundable) FROM v3_billing.funding_lots WHERE source='referral_reward'`).Scan(&knownZero, &locked); err != nil || !knownZero || !locked {
		t.Fatalf("source safeguards=%v/%v %v", knownZero, locked, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username,inviter_id) VALUES(4,'next-invitee',1)`); err != nil {
		t.Fatal(err)
	}
	referralOrderFixture(t, s, 101, 4, "subscription", "standard_v2")
	reserveReferral(t, s, 101)
	fulfillReferral(t, s, 101, 4, "subscription")
	subscriptionReferralFact(t, s, 101, 50000000, 45000000, "next-offset", true)
	*now = now.Add(8 * 24 * time.Hour)
	if _, err = s.SettleReferrals(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if b := referralWalletBalance(t, s, 1); b != 250000 {
		t.Fatalf("future award failed to offset debt: %d", b)
	}
}

func TestV2ReferralUnknownCostAndNegativeContributionNeverPays(t *testing.T) {
	s, pool, now := fixture(t)
	ctx := context.Background()
	enableReferral(t, s, 1000000, 2000000)
	referralOrderFixture(t, s, 100, 2, "subscription", "standard_v2")
	reserveReferral(t, s, 100)
	fulfillReferral(t, s, 100, 2, "subscription")
	subscriptionReferralFact(t, s, 100, 10000000, 0, "unknown-cost", false)
	*now = now.Add(8 * 24 * time.Hour)
	if _, err := s.SettleReferrals(ctx, 100); err != nil {
		t.Fatal(err)
	}
	var state, reason string
	if err := pool.QueryRow(ctx, `SELECT state,reason FROM v3_commerce.referral_consumption_qualifications WHERE order_id=100`).Scan(&state, &reason); err != nil || state != "needs_review" || reason != "cost_unknown" {
		t.Fatalf("unknown=%s/%s %v", state, reason, err)
	}
	if b := referralWalletBalance(t, s, 1); b != 0 {
		t.Fatalf("invented reward=%d", b)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.funding_source_usage SET procurement_cost_amount=11000000 WHERE request_id='unknown-cost'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleReferrals(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if b := referralWalletBalance(t, s, 1); b != 0 {
		t.Fatalf("loss rewarded=%d", b)
	}
}

func TestV2ReferralBudgetReleasesLatePaymentRequiresReviewAndNoRuleReset(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	enableReferral(t, s, 1000000, 1000000)
	referralOrderFixture(t, s, 100, 2, "topup", "legacy")
	reserveReferral(t, s, 100)
	p, err := s.ReferralPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.TotalBudgetCredits = 999999
	if _, err = s.UpdateReferralPolicy(ctx, p); !errors.Is(err, ErrReferralConflict) {
		t.Fatalf("underfunded frozen promise=%v", err)
	}
	if err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.ReleaseReferralTx(ctx, tx, 100) }); err != nil {
		t.Fatal(err)
	}
	fulfillReferral(t, s, 100, 2, "topup")
	var state string
	if err = pool.QueryRow(ctx, `SELECT state FROM v3_commerce.referral_consumption_qualifications WHERE order_id=100`).Scan(&state); err != nil || state != "needs_review" {
		t.Fatalf("late=%s err=%v", state, err)
	}
	if err = s.ApproveReferralReview(ctx, 1); err != nil {
		t.Fatal(err)
	}
	p, err = s.ReferralPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.Enabled = false
	p.EffectiveAt = nil
	if _, err = s.UpdateReferralPolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	p, err = s.ReferralPolicy(ctx)
	if err != nil || p.EffectiveAt == nil {
		t.Fatalf("disable re-enabled legacy resets %+v %v", p, err)
	}
}
