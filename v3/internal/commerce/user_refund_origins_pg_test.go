//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestUserRefundImportedSubscriptionRequiresCumulativeConsumptionProof(t *testing.T) {
	s, refunds, pool, _ := refundServices(t)
	ctx := context.Background()
	plan, err := s.SavePlan(ctx, commerce.Plan{Name: "imported", Currency: "cny", PriceMinor: 1000, Credits: 10_000_000, PeriodSeconds: 3600, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.Create(ctx, commerce.CreateOrder{UserID: 1, PlanID: plan.ID, Provider: "epay", SuccessURL: "https://site.test/ok", CancelURL: "https://site.test/cancel"})
	if err != nil {
		t.Fatal(err)
	}
	var sub, account int64
	if err = pool.QueryRow(ctx, `INSERT INTO v3_commerce.subscriptions(user_id,plan_id,order_id,starts_at,expires_at,total_credits,used_credits)
	 VALUES(1,$1,$2,now()-interval '1 minute',now()+interval '1 hour',10000000,0) RETURNING id`, plan.ID, o.ID).Scan(&sub); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('subscription',$1,'subscription') RETURNING id`, sub).Scan(&account); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET account_id=$2 WHERE id=$1`, sub, account); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_commerce.subscription_buckets(account_id,subscription_id,starts_at) VALUES($1,$2,now())`, account, sub); err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: account, Amount: 10_000_000, Kind: "opening", OperationID: "migration:subscription:1"}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.orders SET state='paid',payment_event_id='imported-sub-order' WHERE id=$1`, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "subscription", TradeNo: o.TradeNo}); !errors.Is(err, commerce.ErrRefundUnavailable) {
		t.Fatalf("erased usage assumed unused: %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_commerce.user_refund_subscription_origins(subscription_id,order_id,used_credits) VALUES($1,$2,7000000)`, sub, o.ID); err != nil {
		t.Fatal(err)
	}
	r, err := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "subscription", TradeNo: o.TradeNo})
	if err != nil || r.Status != "success" || r.AmountMinor != 294 {
		t.Fatalf("cumulative usage ignored: %+v %v", r, err)
	}
}
