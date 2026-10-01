//go:build pgintegration

package commerce_test

import (
	"context"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func TestSubscriptionStarterWindowBonusIsSnapshottedBeforeCheckout(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	unit := credits.Micro(credits.PerCredit)
	starter, err := s.SavePlan(ctx, commerce.Plan{Name: "starter", PlanType: "starter", PriceMinor: 100, Currency: "usd", Credits: unit, DurationUnit: "day", DurationValue: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	starterID, err := s.BindSubscription(ctx, 1, starter.ID, "starter-bonus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET created_at=$2 WHERE id=$1`, starterID, now.Add(-72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	monthly, err := s.SavePlan(ctx, commerce.Plan{Name: "Lite monthly", PlanType: "monthly", PriceMinor: 1000, Currency: "usd", Credits: 1000 * unit, PeriodCredits: 1000 * unit, DurationUnit: "month", DurationValue: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	order := create(t, s, monthly.ID)
	if order.Credits != 1010*unit || order.PeriodCredits != 1010*unit {
		t.Fatalf("72h boundary bonus %+v", order)
	}
	monthly.Name = "Ultra monthly"
	monthly.Credits = 500 * unit
	monthly.PeriodCredits = 100 * unit
	if _, err = s.SavePlan(ctx, monthly); err != nil {
		t.Fatal(err)
	}
	if err = s.Fulfill(ctx, "test", payment(order)); err != nil {
		t.Fatal(err)
	}
	subscriptions, err := s.ListSubscriptions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, sub := range subscriptions {
		if sub.PlanID == monthly.ID {
			found = true
			if sub.TotalCredits != 1010*unit || sub.PeriodCredits != 1010*unit || sub.Balance != 1010*unit {
				t.Fatalf("plan edit changed frozen bonus %+v", sub)
			}
		}
	}
	if !found {
		t.Fatal("monthly subscription missing")
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET created_at=$2 WHERE id=$1`, starterID, now.Add(-72*time.Hour-time.Second)); err != nil {
		t.Fatal(err)
	}
	expired := create(t, s, monthly.ID)
	if expired.Credits != 500*unit || expired.PeriodCredits != 100*unit {
		t.Fatalf("old starter granted new bonus %+v", expired)
	}
}
