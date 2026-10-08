//go:build pgintegration

package incentives

import (
	"context"
	"testing"
	"time"
)

func TestReferralRefundThenLaterConsumptionNeedsExplicitBudgetAndRestoresNetEntitlement(t *testing.T) {
	s, pool, now := fixture(t)
	ctx := context.Background()
	enableReferral(t, s, 100000, 1000000)
	referralOrderFixture(t, s, 100, 2, "subscription", "standard_v2")
	reserveReferral(t, s, 100)
	fulfillReferral(t, s, 100, 2, "subscription")
	subscriptionReferralFact(t, s, 100, 10000000, 9000000, "initial-capped", true)
	*now = now.Add(8 * 24 * time.Hour)
	if _, err := s.SettleReferrals(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.ledger_entries(account_id,amount,balance_after,kind,operation_id,request_id,metadata)
 SELECT account_id,10000000,0,'refund','request-refund','initial-capped','{}' FROM v3_billing.funding_source_usage WHERE request_id='initial-capped'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleReferrals(ctx, 100); err != nil {
		t.Fatal(err)
	}
	subscriptionReferralFact(t, s, 100, 100000000, 80000000, "later-cheaper-route", true)
	*now = now.Add(8 * 24 * time.Hour)
	if _, err := s.SettleReferrals(ctx, 100); err != nil {
		t.Fatal(err)
	}
	var id, debt int64
	var reason string
	if err := pool.QueryRow(ctx, `SELECT id,reason FROM v3_commerce.referral_consumption_qualifications WHERE order_id=100`).Scan(&id, &reason); err != nil || reason != "refund_reinstatement_budget" {
		t.Fatalf("silently capped net entitlement: %s %v", reason, err)
	}
	if err := s.ApproveReferralReview(ctx, id); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := s.SettleReferrals(ctx, 100); err != nil {
			t.Fatal(err)
		}
	}
	if b := referralWalletBalance(t, s, 1); b != 100000 {
		t.Fatalf("reinstatement minted duplicate cash: %d", b)
	}
	if err := pool.QueryRow(ctx, `SELECT debt_credits FROM v3_commerce.referral_consumption_offsets WHERE user_id=1`).Scan(&debt); err != nil || debt != 0 {
		t.Fatalf("restored entitlement leaves debt=%d err=%v", debt, err)
	}
	var net int64
	if err := pool.QueryRow(ctx, `SELECT paid_credits-refunded_reward_credits FROM v3_commerce.referral_consumption_qualifications WHERE id=$1`, id).Scan(&net); err != nil || net != 100000 {
		t.Fatalf("net entitlement=%d err=%v", net, err)
	}
}
