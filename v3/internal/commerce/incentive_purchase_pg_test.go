//go:build pgintegration

package commerce_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/incentives"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestPaidPurchaseReferralAndDeliveryCommitOnce(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	i := incentives.New(pool, ledger.NewPoster(pool), incentives.Config{Now: func() time.Time { return *now }})
	s.SetPaidPurchaseHook(func(ctx context.Context, tx pgx.Tx, o commerce.Order) error {
		return i.PurchaseTx(ctx, tx, incentives.Purchase{UserID: o.UserID, OrderID: o.ID, PlanID: *o.PlanID, AmountMinor: o.AmountMinor, SourceType: "subscription_order", SourceID: o.TradeNo})
	})
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET inviter_id=2 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	p := packagePlan(t, s, 100, 1000000)
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.plans SET plan_type='monthly' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	body, err := json.Marshal(payment(o))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.HandleWebhook(ctx, "test", nil, body); err != nil {
				t.Errorf("concurrent verified payment: %v", err)
			}
		}()
	}
	wg.Wait()
	var available, earned, grants, facts, receipts int64
	if err = pool.QueryRow(ctx, `SELECT available_total,earned_total,
 (SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='subscription_grant'),
 (SELECT count(*) FROM v3_commerce.referral_purchase_rewards WHERE invitee_id=1),
 (SELECT count(*) FROM v3_commerce.payment_events WHERE trade_no=$1)
 FROM v3_commerce.subscription_reset_opportunity_accounts WHERE user_id=2`, o.TradeNo).Scan(&available, &earned, &grants, &facts, &receipts); err != nil {
		t.Fatal(err)
	}
	if available != 1 || earned != 1 || grants != 1 || facts != 1 || receipts != 1 {
		t.Fatalf("purchase/referral replay: available=%d earned=%d grants=%d facts=%d receipts=%d", available, earned, grants, facts, receipts)
	}
	packageCallback(t, s, create(t, s, p.ID))
	if result, err := i.ResetOpportunities(ctx, 2); err != nil || result.AvailableCount != 1 || result.EarnedTotal != 1 {
		t.Fatalf("second purchase rewarded same invitee: %+v err=%v", result, err)
	}
}

func TestPaidPurchaseReferralFailureRollsBackDelivery(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	i := incentives.New(pool, ledger.NewPoster(pool), incentives.Config{Now: func() time.Time { return *now }})
	s.SetPaidPurchaseHook(func(ctx context.Context, tx pgx.Tx, o commerce.Order) error {
		return i.PurchaseTx(ctx, tx, incentives.Purchase{UserID: o.UserID, OrderID: o.ID, PlanID: *o.PlanID, AmountMinor: o.AmountMinor, SourceType: "subscription_order", SourceID: o.TradeNo})
	})
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET inviter_id=2 WHERE id=1;
 INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,available_total,earned_total) VALUES(2,9223372036854775807,9223372036854775807)`); err != nil {
		t.Fatal(err)
	}
	p := packagePlan(t, s, 100, 1000000)
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.plans SET plan_type='monthly' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	o := create(t, s, p.ID)
	if err := s.Fulfill(ctx, "test", payment(o)); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("actual reward failure was accepted: %v", err)
	}
	var state string
	var grants, subscriptions, receipts int64
	if err := pool.QueryRow(ctx, `SELECT state,
 (SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='subscription_grant'),
 (SELECT count(*) FROM v3_commerce.subscriptions),
 (SELECT count(*) FROM v3_commerce.payment_events)
 FROM v3_commerce.orders WHERE id=$1`, o.ID).Scan(&state, &grants, &subscriptions, &receipts); err != nil {
		t.Fatal(err)
	}
	if state != "created" || grants != 0 || subscriptions != 0 || receipts != 0 {
		t.Fatalf("failed reward leaked delivery: state=%s grants=%d subscriptions=%d receipts=%d", state, grants, subscriptions, receipts)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.subscription_reset_opportunity_accounts SET available_total=0,earned_total=0 WHERE user_id=2`); err != nil {
		t.Fatal(err)
	}
	packageCallback(t, s, o)
	if result, err := i.ResetOpportunities(ctx, 2); err != nil || result.AvailableCount != 1 {
		t.Fatalf("verified retry did not recover atomically: %+v err=%v", result, err)
	}
}
