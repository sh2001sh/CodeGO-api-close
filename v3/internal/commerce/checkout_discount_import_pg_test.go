//go:build pgintegration

package commerce_test

import (
	"context"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestCheckoutDiscountImportedCouponBindingConsumptionAndUnknownOriginal(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	card := checkoutCard(t, pool, "topup_discount", 100000)
	if _, err := pool.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET status='reserved',reserved_order_type='topup',reserved_order_trade_no=$2 WHERE id=$1`, card, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	if n, err := s.InitializeImportedCheckoutDiscounts(ctx); err != nil || n != 1 {
		t.Fatalf("import=%d %v", n, err)
	}
	d, err := s.GetCheckoutDiscount(ctx, 1, o.TradeNo)
	if err != nil || d.PropID == nil || *d.PropID != card || d.OriginalKnown || d.PaidMinor != o.AmountMinor {
		t.Fatalf("source coupon=%+v err=%v", d, err)
	}
	if n, err := s.InitializeImportedCheckoutDiscounts(ctx); err != nil || n != 0 {
		t.Fatalf("repeat import=%d %v", n, err)
	}
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM v3_marketplace.blind_box_props WHERE id=$1`, card).Scan(&status); err != nil || status != "used" {
		t.Fatalf("source coupon unused %s %v", status, err)
	}
}

func TestCheckoutDiscountImportedCampaignFreezesExactMetadataAndLateReview(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p := checkoutMonthly(t, s, 1000)
	o := create(t, s, p.ID)
	if _, err := pool.Exec(ctx, `ALTER TABLE v3_commerce.orders
	 ADD COLUMN IF NOT EXISTS discount_applied bool NOT NULL DEFAULT false,
	 ADD COLUMN IF NOT EXISTS discount_multiplier numeric,
	 ADD COLUMN IF NOT EXISTS original_money numeric,
	 ADD COLUMN IF NOT EXISTS discount_starts_at timestamptz,
	 ADD COLUMN IF NOT EXISTS discount_ends_at timestamptz`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.orders SET amount_minor=800,discount_applied=true,discount_multiplier=0.8,original_money=10,
	 discount_starts_at=$2,discount_ends_at=$3 WHERE id=$1`, o.ID, *now, now.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	o, err := s.GetOrder(ctx, 1, o.TradeNo)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.InitializeImportedCheckoutDiscounts(ctx); err != nil || n != 1 {
		t.Fatalf("import=%d %v", n, err)
	}
	d, err := s.GetCheckoutDiscount(ctx, 1, o.TradeNo)
	if err != nil || !d.OriginalKnown || !d.Campaign || d.OriginalMinor != 1000 || d.StartsAt != now.Unix() || d.EndsAt != now.AddDate(0, 0, 1).Unix() {
		t.Fatalf("source metadata=%+v err=%v", d, err)
	}
	if err = s.Cancel(ctx, 1, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecoverCheckoutDiscounts(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if err = s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
	late, err := s.GetOrder(ctx, 1, o.TradeNo)
	if err != nil || late.FulfillmentState != "requires_review" {
		t.Fatalf("imported released campaign granted %+v %v", late, err)
	}
	if err = s.ConfirmRefund(ctx, "test", o.TradeNo, "source-review-refund"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckoutDiscountImportedSourceCorruptionFailsAtomically(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	o := create(t, s, 0)
	first := checkoutCard(t, pool, "topup_discount", 100000)
	second := checkoutCard(t, pool, "topup_discount", 100000)
	if _, err := pool.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET status='reserved',reserved_order_type='topup',reserved_order_trade_no=$3 WHERE id IN($1,$2)`, first, second, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InitializeImportedCheckoutDiscounts(ctx); err == nil {
		t.Fatal("duplicate source coupon bindings accepted")
	}
	if _, err := s.GetCheckoutDiscount(ctx, 1, o.TradeNo); err != commerce.ErrNotFound {
		t.Fatal("corrupt import partially committed", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET status='available',reserved_order_type='',reserved_order_trade_no='' WHERE id=$1`, second); err != nil {
		t.Fatal(err)
	}
	if n, err := s.InitializeImportedCheckoutDiscounts(ctx); err != nil || n != 1 {
		t.Fatalf("repaired import=%d %v", n, err)
	}
}
