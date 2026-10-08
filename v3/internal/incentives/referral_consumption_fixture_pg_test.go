//go:build pgintegration

package incentives

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func enableReferral(t *testing.T, s *Service, cap, budget int64) {
	t.Helper()
	p, err := s.ReferralPolicy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	p.Enabled = true
	p.AncillaryCostPPM = &zero
	p.MaxRewardCredits = credits.Micro(cap)
	p.TotalBudgetCredits = credits.Micro(budget)
	if _, err = s.UpdateReferralPolicy(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}
func referralOrderFixture(t *testing.T, s *Service, id, user int64, kind, version string) {
	t.Helper()
	ctx := context.Background()
	var plan *int64
	period := int64(0)
	if kind == "subscription" {
		value := int64(1)
		plan = &value
		period = 2592000
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO v3_commerce.orders(id,user_id,plan_id,amount_minor,credits,recognized_revenue_credits,period_seconds,currency,kind,provider,trade_no,state,expires_at,created_at,policy_version,plan_snapshot)
 VALUES($1,$2,$3,100,100000000,100000000,$9,'usd',$4,'test',$5,'created',$6,$7,$8,'{"plan_type":"monthly"}')`, id, user, plan, kind, fmt.Sprintf("referral-%d", id), s.now().AddDate(0, 0, 1), s.now(), version, period)
	if err != nil {
		t.Fatal(err)
	}
}
func reserveReferral(t *testing.T, s *Service, id int64) {
	t.Helper()
	if err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error { return s.ReserveReferralTx(context.Background(), tx, id) }); err != nil {
		t.Fatal(err)
	}
}
func fulfillReferral(t *testing.T, s *Service, id, user int64, kind string) {
	t.Helper()
	ctx := context.Background()
	p := Purchase{OrderID: id, UserID: user, AmountMinor: 100, SourceType: "topup_order", SourceID: fmt.Sprintf("referral-%d", id)}
	if kind == "subscription" {
		p.PlanID = 1
		p.SourceType = "subscription_order"
	}
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE v3_commerce.orders SET state='paid',paid_at=$2 WHERE id=$1`, id, s.now()); err != nil {
			return err
		}
		return s.PurchaseTx(ctx, tx, p)
	}); err != nil {
		t.Fatal(err)
	}
}
func subscriptionReferralFact(t *testing.T, s *Service, order, amount, cost int64, request string, known bool) {
	t.Helper()
	ctx := context.Background()
	var account int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('subscription',$1,'subscription') ON CONFLICT(owner_type,owner_id,kind) DO UPDATE SET owner_id=EXCLUDED.owner_id RETURNING id`, order).Scan(&account); err != nil {
		t.Fatal(err)
	}
	var costArg *int64
	if known {
		costArg = &cost
	}
	ppm := int64(1000000)
	if _, err := s.pool.Exec(ctx, `INSERT INTO v3_billing.ledger_entries(account_id,amount,balance_after,kind,operation_id,request_id,metadata) VALUES($1,-$2::bigint,0,'usage',$3,$3,'{}')`, account, amount, request); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO v3_billing.funding_source_usage(request_id,account_id,order_id,policy_version,amount,wallet_equivalent_amount,revenue_multiplier_ppm,procurement_cost_multiplier_ppm,procurement_cost_amount,settled_at)
 VALUES($3,$1,$4,'standard_v2',$2,$2,$5,$5,$6,$7)`, account, amount, request, order, ppm, costArg, s.now()); err != nil {
		t.Fatal(err)
	}
}
func referralWalletBalance(t *testing.T, s *Service, user int64) int64 {
	t.Helper()
	var value int64
	if err := s.pool.QueryRow(context.Background(), `SELECT COALESCE((SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=$1 AND kind='wallet'),0)`, user).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
