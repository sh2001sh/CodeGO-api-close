//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

type checkoutDiscountDecline struct{ fakePayment }

func (checkoutDiscountDecline) Checkout(context.Context, commerce.Order, string, string) (commerce.Checkout, error) {
	return commerce.Checkout{}, commerce.ErrProviderUnavailable
}

type checkoutDiscountFailPoster struct{}

func (checkoutDiscountFailPoster) PostTx(context.Context, pgx.Tx, billing.Entry) (billing.PostResult, error) {
	return billing.PostResult{}, errors.New("injected ledger rollback")
}

func TestCheckoutDiscountOrderTransactionRollbackAndProviderDeclineRelease(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	card := checkoutCard(t, pool, "topup_discount", 100000)
	_, err := pool.Exec(ctx, `CREATE FUNCTION v3_commerce.reject_discount() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected pricing rollback'; END $$;
	 CREATE TRIGGER reject_discount BEFORE INSERT ON v3_commerce.checkout_discounts FOR EACH ROW EXECUTE FUNCTION v3_commerce.reject_discount()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Create(ctx, commerce.CreateOrder{UserID: 1, AmountMinor: 1200, Provider: "test", SuccessURL: "https://site.test/s", CancelURL: "https://site.test/c"}); err == nil {
		t.Fatal("injected order error ignored")
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.orders`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("order persisted count=%d err=%v", count, err)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM v3_marketplace.blind_box_props WHERE id=$1`, card).Scan(&status); err != nil || status != "available" {
		t.Fatalf("rollback status=%s err=%v", status, err)
	}
	if _, err = pool.Exec(ctx, `DROP TRIGGER reject_discount ON v3_commerce.checkout_discounts`); err != nil {
		t.Fatal(err)
	}
	declined := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{checkoutDiscountDecline{}}, commerce.Config{Now: func() time.Time { return *now }, ReturnOrigins: []string{"https://site.test"}})
	o, err := declined.Create(ctx, commerce.CreateOrder{UserID: 1, AmountMinor: 1200, Provider: "test", SuccessURL: "https://site.test/s", CancelURL: "https://site.test/c"})
	if !errors.Is(err, commerce.ErrProviderUnavailable) || o.ID == 0 {
		t.Fatalf("declined %+v %v", o, err)
	}
	if _, err = s.RecoverCheckoutDiscounts(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM v3_marketplace.blind_box_props WHERE id=$1`, card).Scan(&status); err != nil || status != "available" {
		t.Fatalf("decline status=%s err=%v", status, err)
	}
	d, err := s.GetCheckoutDiscount(ctx, 1, o.TradeNo)
	if err != nil || d.State != "released" {
		t.Fatalf("decline snapshot=%+v err=%v", d, err)
	}
}

func TestCheckoutDiscountFulfillmentRollbackLeavesCouponAndReceiptRetryable(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	card := checkoutCard(t, pool, "topup_discount", 100000)
	o := create(t, s, 0)
	failing := commerce.New(pool, checkoutDiscountFailPoster{}, []commerce.PaymentProvider{fakePayment{}}, commerce.Config{Now: func() time.Time { return *now }, ReturnOrigins: []string{"https://site.test"}})
	if err := failing.Fulfill(ctx, "test", payment(o)); err == nil {
		t.Fatal("injected ledger failure ignored")
	}
	d, err := s.GetCheckoutDiscount(ctx, 1, o.TradeNo)
	if err != nil || d.State != "reserved" {
		t.Fatalf("failed grant consumed coupon %+v %v", d, err)
	}
	var status string
	var receipts int
	if err = pool.QueryRow(ctx, `SELECT status,(SELECT count(*) FROM v3_commerce.payment_events) FROM v3_marketplace.blind_box_props WHERE id=$1`, card).Scan(&status, &receipts); err != nil || status != "reserved" || receipts != 0 {
		t.Fatalf("status=%s receipts=%d err=%v", status, receipts, err)
	}
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM v3_marketplace.blind_box_props WHERE id=$1`, card).Scan(&status); err != nil || status != "used" {
		t.Fatalf("retry status=%s err=%v", status, err)
	}
}

func TestCheckoutDiscountCampaignImportedPendingClaimAndFailedClaimRecovery(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p := checkoutMonthly(t, s, 100)
	old := create(t, s, p.ID)
	if _, err := pool.Exec(ctx, `ALTER TABLE v3_commerce.orders ADD COLUMN IF NOT EXISTS discount_applied bool NOT NULL DEFAULT false`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.orders SET discount_applied=true WHERE id=$1`, old.ID); err != nil {
		t.Fatal(err)
	}
	checkoutCampaignSetting(t, pool, *now, "0.8")
	blocked := create(t, s, p.ID)
	if blocked.AmountMinor != 100 {
		t.Fatal("imported pending claim duplicated")
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.orders SET state='failed' WHERE id=$1`, old.ID); err != nil {
		t.Fatal(err)
	}
	first := create(t, s, p.ID)
	if first.AmountMinor != 80 {
		t.Fatal("failed imported claim blocks campaign")
	}
	if err := s.Cancel(ctx, 1, first.TradeNo); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecoverCheckoutDiscounts(ctx, 100); err != nil {
		t.Fatal(err)
	}
	next := create(t, s, p.ID)
	if next.AmountMinor != 80 {
		t.Fatal("failed campaign claim blocks campaign")
	}
	if err := s.Fulfill(ctx, "test", payment(first)); err != nil {
		t.Fatal(err)
	}
	late, err := s.GetOrder(ctx, 1, first.TradeNo)
	if err != nil || late.FulfillmentState != "requires_review" {
		t.Fatalf("released campaign granted %+v %v", late, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.subscriptions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("late campaign subscriptions=%d err=%v", count, err)
	}
}
