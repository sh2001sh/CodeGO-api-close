//go:build pgintegration

package commerce_test

import (
	"context"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestRewardSubscriptionRetainedRenewalAndUpgradeUseFrozenBenefitRules(t *testing.T) {
	for _, action := range []string{"renew", "upgrade"} {
		t.Run(action, func(t *testing.T) {
			s, pool, now := newService(t)
			ctx := context.Background()
			s.SetMonthlyBenefits(rewardMarket(s, pool, now))
			current := rewardMonthlyPlan(t, s, pool, "standard", 1000, 1000, 0)
			initial := create(t, s, current.ID)
			retainMonthlySnapshot(t, pool, initial.ID, 1800, 0)
			packageCallback(t, s, initial)
			before := onlySubscription(t, s)
			spendPackage(t, pool, before.AccountID, 400, "benefit:forty-percent")
			target := current
			want := int64(2520) // Initial 1800 + 40% renewal's 720 seconds.
			if action == "upgrade" {
				target = rewardMonthlyPlan(t, s, pool, "pro", 2000, 2000, 0)
				want = 3420 // Initial 1800 + (2700-1800)+1800*40%.
			}
			o, err := s.Create(ctx, packageRequest(target.ID, before.ID, action, "benefit-"+action))
			if err != nil {
				t.Fatal(err)
			}
			seconds := int64(1800)
			if action == "upgrade" {
				seconds = 2700
			}
			retainMonthlySnapshot(t, pool, o.ID, seconds, 1800)
			if _, err = pool.Exec(ctx, `UPDATE v3_commerce.plans SET membership_tier='ultra',price_minor=9000 WHERE id=$1 OR id=$2`, current.ID, target.ID); err != nil {
				t.Fatal(err)
			}
			packageCallback(t, s, o)
			packageCallback(t, s, o)
			var receipts int64
			if err = pool.QueryRow(ctx, `SELECT remaining_seconds,(SELECT count(*) FROM v3_marketplace.operations WHERE user_id=1 AND kind='monthly_card')
			 FROM v3_marketplace.blind_box_props WHERE user_id=1 AND prop_type='monthly_pass_multiplier'`).Scan(&seconds, &receipts); err != nil {
				t.Fatal(err)
			}
			if seconds != want || receipts != 2 {
				t.Fatalf("paid benefit seconds=%d want=%d receipts=%d", seconds, want, receipts)
			}
		})
	}
}

func TestRewardSubscriptionRestoredLatePaymentAndFuelGrantNoMonthlyCard(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	s.SetMonthlyBenefits(rewardMarket(s, pool, now))
	p := rewardMonthlyPlan(t, s, pool, "standard", 1000, 1000, 0)
	p.FuelEnabled, p.FuelUnitPriceMicro, p.FuelMinCredits, p.FuelCreditStep = true, 10_000_000_000, 100, 100
	p.MembershipTier = "standard"
	if _, err := s.SavePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	initial := create(t, s, p.ID)
	retainMonthlySnapshot(t, pool, initial.ID, 1800, 0)
	packageCallback(t, s, initial)
	before := onlySubscription(t, s)
	fuel, err := s.Create(ctx, commerce.CreateOrder{UserID: 1, Provider: "test", PurchaseType: "fuel", TargetSubscriptionID: before.ID, FuelCredits: 100,
		SuccessURL: "https://site.test/success", CancelURL: "https://site.test/cancel"})
	if err != nil {
		t.Fatal(err)
	}
	packageCallback(t, s, fuel)
	spendPackage(t, pool, before.AccountID, 440, "benefit:after-fuel")
	o, err := s.Create(ctx, packageRequest(p.ID, before.ID, "renew", "benefit-late"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Cancel(ctx, 1, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	if err = s.RestorePackageCheckout(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	packageCallback(t, s, o)
	var seconds int64
	if err = pool.QueryRow(ctx, `SELECT remaining_seconds FROM v3_marketplace.blind_box_props WHERE user_id=1 AND prop_type='monthly_pass_multiplier'`).Scan(&seconds); err != nil {
		t.Fatal(err)
	}
	if seconds != 1800 {
		t.Fatalf("fuel or reviewed payment granted monthly card seconds=%d", seconds)
	}
}
