//go:build pgintegration

package commerce_test

import (
	"context"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestRetiredLuckyNumbersAreNotIssuedByPurchaseGrantOrRedemption(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "eligible lucky monthly", PlanType: "monthly", PriceMinor: 100, Currency: "usd", Credits: 1000000, DurationUnit: "month", DurationValue: 1, Enabled: true, LuckyDrawEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := s.ListPlans(ctx, true)
	if err != nil || len(stored) != 1 || !stored[0].LuckyDrawEnabled {
		t.Fatalf("lucky eligibility was not persisted: %+v err=%v", stored, err)
	}
	order := create(t, s, p.ID)
	packageCallback(t, s, order)
	packageCallback(t, s, order)
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.subscription_lucky_numbers`); n != 0 {
		t.Fatalf("purchase generated retired lucky numbers: %d", n)
	}
	admin, err := s.BindSubscription(ctx, 1, p.ID, "lucky-admin-assignment")
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := s.BindSubscription(ctx, 1, p.ID, "lucky-admin-assignment"); err != nil || replay != admin {
		t.Fatalf("admin assignment replay: id=%d err=%v", replay, err)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.subscription_lucky_numbers`); n != 0 {
		t.Fatalf("grant generated retired lucky numbers: %d", n)
	}
	code, err := s.IssueTypedRedemption(ctx, commerce.IssueRedemptionInput{Name: "lucky package", RedeemType: "subscription", PlanID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.RedeemTyped(ctx, 1, code.Key)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := s.RedeemTyped(ctx, 1, code.Key); err != nil || replay != result {
		t.Fatalf("redemption assignment replay: %+v err=%v", replay, err)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.subscription_lucky_numbers`); n != 0 {
		t.Fatalf("redemption generated retired lucky numbers: %d", n)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.subscription_lucky_numbers n JOIN v3_commerce.subscriptions s ON s.id=n.subscription_id WHERE n.user_id<>s.user_id OR n.lucky_suffix!~'^[0-9]{4}$' OR n.card_code NOT LIKE 'CG-%'`); n != 0 {
		t.Fatalf("lucky number ownership/format invalid: %d", n)
	}
	p.LuckyDrawEnabled = false
	if _, err := s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BindSubscription(ctx, 1, p.ID, "lucky-disabled-assignment"); err != nil {
		t.Fatal(err)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.subscription_lucky_numbers`); n != 0 {
		t.Fatalf("disabled plan generated a lucky number: %d", n)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.referral_purchase_rewards`); n != 0 {
		t.Fatalf("nonreferred or unpaid grant generated referral reward: %d", n)
	}
}

func TestRetiredLuckyTableCannotBlockSubscriptionDelivery(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p, err := s.SavePlan(ctx, commerce.Plan{Name: "legacy monthly", PlanType: "monthly", PriceMinor: 100, Currency: "usd", Credits: 1000000, DurationUnit: "month", DurationValue: 1, Enabled: true, LuckyDrawEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	code, err := s.IssueTypedRedemption(ctx, commerce.IssueRedemptionInput{Name: "legacy package", RedeemType: "subscription", PlanID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE v3_commerce.subscription_lucky_numbers ADD CONSTRAINT refuse_lucky_assignment CHECK(user_id<>1)`); err != nil {
		t.Fatal(err)
	}
	if result, err := s.RedeemTyped(ctx, 1, code.Key); err != nil || result.UserSubscriptionID <= 0 {
		t.Fatalf("retired lucky table blocked delivery: %+v err=%v", result, err)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.subscription_lucky_numbers`); n != 0 {
		t.Fatalf("delivery generated retired numbers: %d", n)
	}
}
