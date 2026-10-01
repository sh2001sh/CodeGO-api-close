//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestSubscriptionUnfulfilledPaidAndRefundedReviewsRestoreFrozenSource(t *testing.T) {
	for _, state := range []string{"paid", "refunded"} {
		t.Run(state, func(t *testing.T) {
			s, pool, _ := newService(t)
			ctx := context.Background()
			plan := packagePlan(t, s, 1000, 1000)
			packageCallback(t, s, create(t, s, plan.ID))
			sub := onlySubscription(t, s)
			spendPackage(t, pool, sub.AccountID, 400, "review-restore:used")
			quote, err := s.CreatePackageCheckout(ctx, packageRequest(plan.ID, sub.ID, "renew", "review-restore"))
			if err != nil {
				t.Fatal(err)
			}
			// Model the transactionally recorded outcome from the group-room or
			// coupon validator. The signed payment's state must remain authoritative.
			if _, err = pool.Exec(ctx, `UPDATE v3_commerce.orders SET state=$2,fulfillment_state='completed' WHERE id=$1`, quote.ID, state); err != nil {
				t.Fatal(err)
			}
			if err = s.RestorePackageCheckout(ctx, quote.ID); !errors.Is(err, commerce.ErrStateConflict) {
				t.Fatalf("completed payment was undone: %v", err)
			}
			if _, err = pool.Exec(ctx, `UPDATE v3_commerce.orders SET fulfillment_state='requires_review' WHERE id=$1`, quote.ID); err != nil {
				t.Fatal(err)
			}
			if n, err := s.RecoverPackageCheckouts(ctx, 100); err != nil || n != 1 {
				t.Fatalf("unfulfilled review recovery %d %v", n, err)
			}
			restored := onlySubscription(t, s)
			if restored.Balance != 600 || restored.UsedCredits != 400 || restored.AccountID == sub.AccountID {
				t.Fatalf("source funds lost after review %+v", restored)
			}
			order, err := s.GetOrder(ctx, 1, quote.TradeNo)
			if err != nil || order.State != state || order.FulfillmentState != "requires_review" {
				t.Fatalf("payment evidence rewritten %+v %v", order, err)
			}
			if n, err := s.RecoverPackageCheckouts(ctx, 100); err != nil || n != 0 {
				t.Fatalf("restoration replay %d %v", n, err)
			}
		})
	}
}

func TestSubscriptionUnfulfilledImportedTargetWithoutFrozenQuoteRequiresReview(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	plan := packagePlan(t, s, 1000, 1000)
	packageCallback(t, s, create(t, s, plan.ID))
	sub := onlySubscription(t, s)
	spendPackage(t, pool, sub.AccountID, 400, "import-review:used")
	o := create(t, s, plan.ID)
	// Imported pending orders can retain a target but lack a native frozen
	// quote. An authenticated payment cannot invent that missing monetary proof.
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.orders SET target_subscription_id=$2 WHERE id=$1`, o.ID, sub.ID); err != nil {
		t.Fatal(err)
	}
	packageCallback(t, s, o)
	packageCallback(t, s, o)
	after := onlySubscription(t, s)
	if after.AccountID != sub.AccountID || after.Balance != 600 || after.UsedCredits != 400 {
		t.Fatalf("unproven imported intent changed the entitlement %+v", after)
	}
	paid, err := s.GetOrder(ctx, 1, o.TradeNo)
	if err != nil || paid.State != "paid" || paid.FulfillmentState != "requires_review" {
		t.Fatalf("missing quote did not preserve payment/review %+v %v", paid, err)
	}
	reviews, err := s.ListPackagePaymentReviews(ctx)
	if err != nil || len(reviews) != 1 || reviews[0].OrderID != o.ID {
		t.Fatalf("durable imported review %+v %v", reviews, err)
	}
}
