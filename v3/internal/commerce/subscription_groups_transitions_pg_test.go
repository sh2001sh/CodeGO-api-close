//go:build pgintegration

package commerce_test

import (
	"context"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestSubscriptionGroupsRenewUpgradePreserveOriginalGroup(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	initial := subscriptionGroupPlan(t, s, pool, "vip")
	packageCallback(t, s, create(t, s, initial.ID))
	sub := onlySubscription(t, s)
	spendPackage(t, pool, sub.AccountID, 400, "group-renew-usage")
	renew, err := s.Create(ctx, packageRequest(initial.ID, sub.ID, "renew", "group-renew"))
	if err != nil {
		t.Fatal(err)
	}
	packageCallback(t, s, renew)
	assertSubscriptionGroup(t, pool, sub.ID, "vip", "vip", "default")
	target := subscriptionGroupPlan(t, s, pool, "premium")
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.plans SET price_minor=2000,credits=2000 WHERE id=$1`, target.ID); err != nil {
		t.Fatal(err)
	}
	upgrade, err := s.Create(ctx, packageRequest(target.ID, sub.ID, "upgrade", "group-upgrade"))
	if err != nil {
		t.Fatal(err)
	}
	packageCallback(t, s, upgrade)
	packageCallback(t, s, upgrade)
	assertSubscriptionGroup(t, pool, sub.ID, "premium", "premium", "default")
	if err = s.EndSubscription(ctx, sub.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	assertSubscriptionGroup(t, pool, sub.ID, "default", "premium", "default")
}

func TestSubscriptionGroupsAdminReactivationUsesSavedPolicy(t *testing.T) {
	s, pool, now := newService(t)
	ctx := context.Background()
	p := subscriptionGroupPlan(t, s, pool, "vip")
	packageCallback(t, s, create(t, s, p.ID))
	sub := onlySubscription(t, s)
	if err := s.EndSubscription(ctx, sub.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	assertSubscriptionGroup(t, pool, sub.ID, "default", "vip", "default")
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.plans SET upgrade_group='premium' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	in := commerce.EditSubscription{StartsAt: *now, ExpiresAt: now.Add(time.Hour), State: "active", TotalCredits: 1000, RequestID: "reactivate-group"}
	if err := s.UpdateSubscription(ctx, sub.ID, 1, in); err != nil {
		t.Fatal(err)
	}
	assertSubscriptionGroup(t, pool, sub.ID, "vip", "vip", "default")
	in.State, in.RequestID = "expired", "deactivate-group"
	if err := s.UpdateSubscription(ctx, sub.ID, 1, in); err != nil {
		t.Fatal(err)
	}
	assertSubscriptionGroup(t, pool, sub.ID, "default", "vip", "default")
}

func TestSubscriptionGroupsConversionRetainsPartialAndRestoresFinal(t *testing.T) {
	s, pool, _ := newService(t)
	ctx := context.Background()
	p := subscriptionGroupPlan(t, s, pool, "vip")
	packageCallback(t, s, create(t, s, p.ID))
	sub := onlySubscription(t, s)
	if _, err := s.ConvertSubscription(ctx, 1, sub.ID, 50, "group-partial"); err != nil {
		t.Fatal(err)
	}
	assertSubscriptionGroup(t, pool, sub.ID, "vip", "vip", "default")
	if _, err := s.ConvertSubscription(ctx, 1, sub.ID, 50, "group-final"); err != nil {
		t.Fatal(err)
	}
	assertSubscriptionGroup(t, pool, sub.ID, "default", "vip", "default")
}

func TestSubscriptionGroupsUserRefundFailureRestoresSavedPolicy(t *testing.T) {
	s, refunds, pool, provider := refundServices(t)
	ctx := context.Background()
	p := subscriptionGroupPlan(t, s, pool, "vip")
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.plans SET currency='cny' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	o := paidRefundOrder(t, s, 1000, p.ID)
	sub := onlySubscription(t, s)
	assertSubscriptionGroup(t, pool, sub.ID, "vip", "vip", "default")
	provider.state = "processing"
	r, err := refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "subscription", TradeNo: o.TradeNo})
	if err != nil || r.Status != "processing" {
		t.Fatalf("refund reserve=%+v err=%v", r, err)
	}
	assertSubscriptionGroup(t, pool, sub.ID, "default", "vip", "default")
	if _, err = pool.Exec(ctx, `UPDATE v3_commerce.plans SET upgrade_group='premium' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	provider.state = "failed"
	if r, err = refunds.Sync(ctx, 1, r.RefundNo); err != nil || r.Status != "failed" {
		t.Fatalf("refund failure=%+v err=%v", r, err)
	}
	assertSubscriptionGroup(t, pool, sub.ID, "vip", "vip", "default")
	provider.state = "success"
	if r, err = refunds.Create(ctx, 1, commerce.UserRefundRequest{OrderType: "subscription", TradeNo: o.TradeNo}); err != nil || r.Status != "success" {
		t.Fatalf("refund success=%+v err=%v", r, err)
	}
	assertSubscriptionGroup(t, pool, sub.ID, "default", "vip", "default")
}
