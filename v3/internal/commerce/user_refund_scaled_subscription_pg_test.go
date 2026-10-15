//go:build pgintegration

package commerce_test

import (
	"context"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestUserRefundAfterQuotaReductionUsesOriginalPaidRatio(t *testing.T) {
	s, refunds, pool, _ := refundServices(t)
	ctx := context.Background()
	plan, err := s.SavePlan(ctx, commerce.Plan{Name: "scaled", Currency: "cny", PriceMinor: 1000, Credits: 10_000_000, PeriodSeconds: 3600, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	o := paidRefundOrder(t, s, 1000, plan.ID)
	subs, err := s.ListSubscriptions(ctx, 1)
	if err != nil || len(subs) != 1 {
		t.Fatalf("subscriptions=%d err=%v", len(subs), err)
	}
	sub := subs[0]
	err = s.UpdateSubscription(ctx, sub.ID, 1, commerce.EditSubscription{
		StartsAt: sub.StartsAt, ExpiresAt: sub.ExpiresAt, State: "active",
		TotalCredits: 1_030_000, UsedCredits: 515_000, RequestID: "scaled-refund-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	subs, err = s.ListSubscriptions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	refundPost(t, pool, subs[0].AccountID, -103_000, "usage", "scaled-subscription-spend")
	items, err := refunds.Eligible(ctx, 1)
	if err != nil || len(items) != 1 || items[0].RemainingQuota != 4_000_000 || items[0].RefundAmountMinor != 392 {
		t.Fatalf("scaled quotes=%+v err=%v", items, err)
	}
	r, err := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "subscription", TradeNo: o.TradeNo})
	if err != nil || r.Status != "success" || r.AmountMinor != 392 {
		t.Fatalf("scaled refund=%+v err=%v", r, err)
	}
	subs, err = s.ListSubscriptions(ctx, 1)
	if err != nil || subs[0].Balance != 0 || subs[0].State != "canceled" {
		t.Fatalf("refunded quota remains usable: %+v err=%v", subs, err)
	}
}
