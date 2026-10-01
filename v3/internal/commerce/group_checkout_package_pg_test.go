//go:build pgintegration

package commerce_test

import (
	"context"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func groupPackageSetup(t *testing.T) (*groupFixture, commerce.Plan, commerce.Subscription, int64) {
	t.Helper()
	f := newGroupFixture(t)
	p := f.plan(t, 2)
	p.GroupBuyEnabled = false
	var err error
	p, err = f.s.SavePlan(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	first, code, body := f.checkout(1, map[string]any{"plan_id": p.ID}, false)
	if code != 200 {
		t.Fatalf("base checkout=%d %s", code, body)
	}
	if err = f.pay(first); err != nil {
		t.Fatal(err)
	}
	before := onlySubscription(t, f.s)
	spendPackage(t, f.pool, before.AccountID, 400, "group-renewal-usage")
	p.GroupBuyEnabled = true
	p, err = f.s.SavePlan(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	founder, code, body := f.checkout(2, map[string]any{"plan_id": p.ID}, false)
	if code != 200 {
		t.Fatalf("founder checkout=%d %s", code, body)
	}
	if err = f.pay(founder); err != nil {
		t.Fatal(err)
	}
	return f, p, before, f.room(t, founder.ID)
}

func TestGroupCheckoutRenewalKeepsQuoteAndMapsMemberToCurrentOrder(t *testing.T) {
	f, p, before, id := groupPackageSetup(t)
	fields := map[string]any{"plan_id": p.ID, "purchase_type": "join_group", "group_buy_id": id,
		"purchase_action": "renew", "target_subscription_id": before.ID, "request_id": "renew-group-once"}
	o, code, body := f.checkout(1, fields, false)
	if code != 200 || o.AmountMinor != 400 || o.PurchaseType != "join_group" || o.TargetSubscriptionID != before.ID {
		t.Fatalf("group mode bypassed durable renewal quote: order=%+v status=%d body=%s", o, code, body)
	}
	if err := f.pay(o); err != nil {
		t.Fatal(err)
	}
	if err := f.pay(o); err != nil {
		t.Fatal(err)
	}
	after := onlySubscription(t, f.s)
	if before.ID != after.ID || before.AccountID == after.AccountID || after.Balance != 1100 || after.TotalCredits != 1100 || after.UsedCredits != 0 {
		t.Fatalf("renewed group entitlement=%+v before=%+v", after, before)
	}
	var memberSubscription, memberAccount int64
	if err := f.pool.QueryRow(context.Background(), `SELECT subscription_id,account_id FROM v3_marketplace.group_buy_members WHERE order_id=$1`, o.ID).Scan(&memberSubscription, &memberAccount); err != nil || memberSubscription != after.ID || memberAccount != after.AccountID {
		t.Fatalf("renewed member references stale subscription/account: sub=%d account=%d error=%v", memberSubscription, memberAccount, err)
	}
	if f.room(t, o.ID) != id {
		t.Fatal("renewal created different room")
	}
	fields["group_buy_id"] = id + 1000
	if _, status, text := f.checkout(1, fields, false); status == 200 {
		t.Fatalf("same request key changed group target: %s", text)
	}
}

func TestGroupCheckoutFullRoomReviewRefundRestoresOldPackage(t *testing.T) {
	f, p, before, id := groupPackageSetup(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(3,'last-member')`); err != nil {
		t.Fatal(err)
	}
	renewal, code, body := f.checkout(1, map[string]any{"plan_id": p.ID, "purchase_type": "join_group", "group_buy_id": id,
		"purchase_action": "renew", "target_subscription_id": before.ID, "request_id": "full-room-renewal"}, false)
	if code != 200 || renewal.AmountMinor != 400 {
		t.Fatalf("renewal quote=%+v status=%d body=%s", renewal, code, body)
	}
	last, code, body := f.checkout(3, map[string]any{"plan_id": p.ID, "purchase_type": "join_group", "group_buy_id": id}, false)
	if code != 200 {
		t.Fatalf("last checkout=%d %s", code, body)
	}
	if err := f.pay(last); err != nil {
		t.Fatal(err)
	}
	if err := f.pay(renewal); err != nil {
		t.Fatal(err)
	}
	order, err := f.s.GetOrder(ctx, 1, renewal.TradeNo)
	if err != nil || order.State != "paid" || order.FulfillmentState != "requires_review" {
		t.Fatalf("stale paid room not reconciled: order=%+v error=%v", order, err)
	}
	if err = f.s.ResolvePackagePaymentReview(ctx, renewal.ID); err == nil {
		t.Fatal("browser/admin acknowledgement fabricated a refund")
	}
	if err = f.s.ConfirmRefund(ctx, "epay", renewal.TradeNo, "verified-group-review-refund"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.RecoverPackageCheckouts(ctx, 100); err != nil {
		t.Fatal(err)
	}
	after := onlySubscription(t, f.s)
	if after.ID != before.ID || after.AccountID == before.AccountID || after.Balance != 600 || after.TotalCredits != 1000 || after.UsedCredits != 400 || !after.ExpiresAt.Equal(before.ExpiresAt) {
		t.Fatalf("review/refund altered old package or left old frozen bucket: before=%+v after=%+v", before, after)
	}
	var members int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM v3_marketplace.group_buy_members WHERE user_id=1`).Scan(&members); err != nil || members != 0 {
		t.Fatalf("review fabricated group member: count=%d error=%v", members, err)
	}
	if err = f.s.ResolvePackagePaymentReview(ctx, renewal.ID); err != nil {
		t.Fatal(err)
	}
}
