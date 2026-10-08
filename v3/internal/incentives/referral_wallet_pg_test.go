//go:build pgintegration

package incentives

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

func TestWalletReferralOnlyActualPaidLotReceivesProportionalCost(t *testing.T) {
	s, pool, now := fixture(t)
	ctx := context.Background()
	enableReferral(t, s, 2000000, 5000000)
	referralOrderFixture(t, s, 100, 2, "topup", "legacy")
	reserveReferral(t, s, 100)
	fulfillReferral(t, s, 100, 2, "topup")
	var wallet int64
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',2,'wallet') RETURNING id`).Scan(&wallet); err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := s.poster.PostTx(ctx, tx, billing.Entry{AccountID: wallet, Amount: 100000000, Kind: "topup", OperationID: "order:paid:referral-100", Metadata: map[string]any{"order_id": 100}})
		if err != nil {
			return err
		}
		_, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: wallet, Amount: 50000000, Kind: "reward", Reason: "referral_reward", OperationID: "gift", Metadata: map[string]any{"source": "referral_reward"}})
		if err != nil {
			return err
		}
		_, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: wallet, Amount: -120000000, Kind: "usage", OperationID: "wallet-usage", RequestID: "wallet-usage"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.funding_source_usage(request_id,account_id,policy_version,amount,wallet_equivalent_amount,procurement_cost_amount,settled_at) VALUES('wallet-usage',$1,'wallet',120000000,120000000,108000000,$2)`, wallet, s.now()); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(8 * 24 * time.Hour)
	if _, err := s.SettleReferrals(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if got := referralWalletBalance(t, s, 1); got != 1000000 {
		t.Fatalf("rewarded gifted consumption or wrong cost: %d", got)
	}
	// Refund only half the paid external amount. Original usage is 100 worth,
	// remaining realized revenue is capped to 50; upstream expense stays 90.
	if _, err := pool.Exec(ctx, `INSERT INTO v3_commerce.provider_refund_progress(order_id,amount_minor,reversed_credits,updated_at) VALUES(100,50,50000000,$1)`, s.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleReferrals(ctx, 100); err != nil {
		t.Fatal(err)
	}
	var debt int64
	if err := pool.QueryRow(ctx, `SELECT debt_credits FROM v3_commerce.referral_consumption_offsets WHERE user_id=1`).Scan(&debt); err != nil || debt != 1000000 {
		t.Fatalf("partial chargeback offset=%d err=%v", debt, err)
	}
	if got := referralWalletBalance(t, s, 1); got != 1000000 {
		t.Fatalf("refund removed paid wallet money: %d", got)
	}
}

func TestWalletReferralMissingProcurementFactsRequiresReview(t *testing.T) {
	s, pool, now := fixture(t)
	ctx := context.Background()
	enableReferral(t, s, 2000000, 5000000)
	referralOrderFixture(t, s, 100, 2, "topup", "legacy")
	reserveReferral(t, s, 100)
	fulfillReferral(t, s, 100, 2, "topup")
	var wallet int64
	if err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',2,'wallet') RETURNING id`).Scan(&wallet); err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := s.poster.PostTx(ctx, tx, billing.Entry{AccountID: wallet, Amount: 100000000, Kind: "topup", OperationID: "order:paid:referral-100"})
		if err != nil {
			return err
		}
		_, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: wallet, Amount: -10000000, Kind: "usage", OperationID: "unknown-wallet-cost", RequestID: "unknown-wallet-cost"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Simulate late delivery; usage metadata date is actual event time in real
	// source-v2 rows. A source-v1 row has no frozen procurement amount.
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.ledger_entries SET created_at=$1 WHERE request_id='unknown-wallet-cost'`, s.now()); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(8 * 24 * time.Hour)
	if _, err := s.SettleReferrals(ctx, 100); err != nil {
		t.Fatal(err)
	}
	var reason string
	if err := pool.QueryRow(ctx, `SELECT reason FROM v3_commerce.referral_consumption_qualifications WHERE order_id=100`).Scan(&reason); err != nil || reason != "cost_unknown" {
		t.Fatalf("missing cost=%s err=%v", reason, err)
	}
	if got := referralWalletBalance(t, s, 1); got != 0 {
		t.Fatalf("unknown cost invented margin: %d", got)
	}
}
