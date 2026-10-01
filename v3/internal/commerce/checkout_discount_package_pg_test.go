//go:build pgintegration

package commerce_test

import (
	"context"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestCheckoutDiscountRenewalAppliesAfterRatioOnceAndRestoresOnReleasedLatePayment(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := packagePlan(t, s, 1000, 1000)
	packageCallback(t, s, create(t, s, p.ID))
	before := onlySubscription(t, s)
	spendPackage(t, pool, before.AccountID, 400, "coupon-renew:usage")
	checkoutCard(t, pool, "subscription_discount", 100000)
	in := packageRequest(p.ID, before.ID, "renew", "discounted-renew")
	o, err := s.Create(ctx, in)
	if err != nil || o.AmountMinor != 360 {
		t.Fatalf("ratio thencoupon %+v %v", o, err)
	}
	d, err := s.GetCheckoutDiscount(ctx, 1, o.TradeNo)
	if err != nil || d.OriginalMinor != 400 || d.PaidMinor != 360 {
		t.Fatalf("quote snapshot %+v %v", d, err)
	}
	replay, err := s.Create(ctx, in)
	if err != nil || replay.AmountMinor != 360 || replay.ID != o.ID {
		t.Fatalf("quote discounted again %+v %v", replay, err)
	}
	if err = s.Cancel(ctx, 1, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecoverCheckoutDiscounts(ctx, 100); err != nil {
		t.Fatal(err)
	}
	packageCallback(t, s, o)
	late, err := s.GetOrder(ctx, 1, o.TradeNo)
	if err != nil || late.FulfillmentState != "requires_review" {
		t.Fatalf("late discountedrenew granted %+v %v", late, err)
	}
	if err = s.ConfirmRefund(ctx, "test", o.TradeNo, "discounted-renew-review-refund"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecoverPackageCheckouts(ctx, 100); err != nil {
		t.Fatal(err)
	}
	after := onlySubscription(t, s)
	if after.Balance != 600 || after.UsedCredits != 400 || !after.ExpiresAt.Equal(before.ExpiresAt) {
		t.Fatalf("old package changed %+v", after)
	}
}

func TestCheckoutDiscountFuelDoesNotReserveSubscriptionCoupon(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := checkoutMonthly(t, s, 1000)
	p.FuelEnabled = true
	p.FuelUnitPriceMicro = 100000
	p.FuelMinCredits = credits.Micro(credits.PerCredit)
	p.FuelCreditStep = credits.Micro(credits.PerCredit)
	if _, err := s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	packageCallback(t, s, create(t, s, p.ID))
	sub := onlySubscription(t, s)
	card := checkoutCard(t, pool, "subscription_discount", 100000)
	o, err := s.Create(ctx, commerce.CreateOrder{UserID: 1, PurchaseType: "fuel", TargetSubscriptionID: sub.ID, FuelCredits: 10 * credits.Micro(credits.PerCredit), Provider: "test", SuccessURL: "https://site.test/s", CancelURL: "https://site.test/c"})
	if err != nil || o.AmountMinor != 100 {
		t.Fatalf("fuel coupon %+v %v", o, err)
	}
	if _, err = s.GetCheckoutDiscount(ctx, 1, o.TradeNo); err != commerce.ErrNotFound {
		t.Fatal("fuel discount created", err)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM v3_marketplace.blind_box_props WHERE id=$1`, card).Scan(&status); err != nil || status != "available" {
		t.Fatalf("fuel reserved coupon %s %v", status, err)
	}
}

func TestCheckoutDiscountUpgradeCampaignUsesProportionalQuoteAndSkipsCoupon(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	old := packagePlan(t, s, 1001, 1000)
	packageCallback(t, s, create(t, s, old.ID))
	before := onlySubscription(t, s)
	spendPackage(t, pool, before.AccountID, 333, "campaign-upgrade:usage")
	checkoutCampaignSetting(t, pool, *now, "0.8")
	card := checkoutCard(t, pool, "subscription_discount", 100000)
	p := checkoutMonthly(t, s, 2000)
	o, err := s.Create(ctx, packageRequest(p.ID, before.ID, "upgrade", "campaign-upgrade"))
	if err != nil || o.AmountMinor != 1066 || o.Credits != p.Credits {
		t.Fatalf("upgradecampaign %+v %v", o, err)
	}
	d, err := s.GetCheckoutDiscount(ctx, 1, o.TradeNo)
	if err != nil || d.OriginalMinor != 1332 || !d.Campaign || d.PropID != nil {
		t.Fatalf("upgrade frozen %+v %v", d, err)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM v3_marketplace.blind_box_props WHERE id=$1`, card).Scan(&status); err != nil || status != "available" {
		t.Fatalf("campaign stackedcoupon %s %v", status, err)
	}
	packageCallback(t, s, o)
	after := onlySubscription(t, s)
	if after.Balance != p.Credits || after.TotalCredits != p.Credits {
		t.Fatalf("discount changedbenefits %+v", after)
	}
	if _, err = s.GetCheckoutDiscount(ctx, 2, o.TradeNo); err != commerce.ErrNotFound {
		t.Fatal(err)
	}
}
