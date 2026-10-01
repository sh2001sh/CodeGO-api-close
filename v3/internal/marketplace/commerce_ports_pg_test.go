//go:build pgintegration

package marketplace_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

type checkoutProvider struct{}

func (checkoutProvider) Name() string { return "test" }
func (checkoutProvider) Checkout(_ context.Context, o commerce.Order, _, _ string) (commerce.Checkout, error) {
	return commerce.Checkout{Reference: o.TradeNo, URL: "https://payment.example/checkout"}, nil
}
func (checkoutProvider) Verify(context.Context, http.Header, []byte) (commerce.PaymentEvent, error) {
	return commerce.PaymentEvent{}, fmt.Errorf("test provider does not accept HTTP callbacks")
}

func TestCommercePortsRewardSubscriptionAndGroupBonus(t *testing.T) {
	ctx := context.Background()
	pool, poster, accounts, cfg := marketplace.DependenciesForIntegrationTest(t)
	cs := commerce.New(pool, poster, []commerce.PaymentProvider{checkoutProvider{}}, commerce.Config{Now: cfg.Now, ReturnOrigins: []string{"https://app.example"}})
	ms := marketplace.New(pool, poster, accounts, cs, cs, cfg)
	cs.SetGroupCheckoutMarket(ms)
	cs.SetMonthlyBenefits(ms)
	p, err := cs.SavePlan(ctx, commerce.Plan{Name: "monthly", PriceMinor: 100, Currency: "usd", Credits: 5000, PeriodSeconds: 30 * 24 * 3600, Enabled: true, GroupBuyEnabled: true, GroupBuyTarget: 2, GroupBuyBonus: 500, GroupBuyLifetimeSeconds: 3600, PlanType: "monthly", MembershipTier: "pro"})
	if err != nil {
		t.Fatal(err)
	}
	var orders []commerce.Order
	for user := int64(1); user <= 2; user++ {
		o, err := cs.Create(ctx, commerce.CreateOrder{UserID: user, PlanID: p.ID, Provider: "test", SuccessURL: "https://app.example/success", CancelURL: "https://app.example/cancel"})
		if err != nil {
			t.Fatal(err)
		}
		if err := cs.Fulfill(ctx, "test", commerce.PaymentEvent{ID: fmt.Sprintf("event%d", user), TradeNo: o.TradeNo, Reference: o.TradeNo, AmountMinor: o.AmountMinor, Currency: o.Currency, Paid: true}); err != nil {
			t.Fatal(err)
		}
		orders = append(orders, o)
	}
	g, err := ms.CreateGroup(ctx, 1, orders[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.JoinGroup(ctx, 2, g.ID, orders[1].ID); err != nil {
		t.Fatal(err)
	}
	for user := int64(1); user <= 2; user++ {
		subs, err := cs.ListSubscriptions(ctx, user)
		if err != nil || len(subs) != 1 || subs[0].Balance != 5500 || subs[0].TotalCredits != 5500 {
			t.Fatalf("subscription bonus user%d=%+v %v", user, subs, err)
		}
		account, err := accounts.WalletAccount(ctx, user)
		if err != nil {
			t.Fatal(err)
		}
		balance, _, err := accounts.LedgerBalance(ctx, account)
		if err != nil || balance != 10000 {
			t.Fatalf("group bonus changed wallet: %d %v", balance, err)
		}
	}
	box, err := ms.SavePool(ctx, marketplace.Pool{Name: "month-card", Enabled: true, Price: 100, DailyLimit: 100, Rewards: []marketplace.Reward{{Kind: "subscription", Title: "month", Weight: 1, PlanID: p.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.PurchaseBoxes(ctx, 1, "purchase", box.ID, 1); err != nil {
		t.Fatal(err)
	}
	draws, err := ms.OpenBoxes(ctx, 1, "open", 1)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := ms.UseProp(ctx, 1, draws[0].PropID); err != nil {
			t.Fatal(err)
		}
	}
	subs, err := cs.ListSubscriptions(ctx, 1)
	if err != nil || len(subs) != 1 || subs[0].Balance != 10500 || subs[0].TotalCredits != 10500 {
		t.Fatalf("monthly prop did not merge exactly once: %+v %v", subs, err)
	}
	for user := int64(1); user <= 2; user++ {
		var count, remaining int64
		if err := pool.QueryRow(ctx, `SELECT count(*),COALESCE(sum(remaining_seconds),0) FROM v3_marketplace.blind_box_props WHERE user_id=$1 AND prop_type='monthly_pass_multiplier'`, user).Scan(&count, &remaining); err != nil {
			t.Fatal(err)
		}
		want := int64(2700)
		if user == 1 {
			want *= 2
		}
		if count != 1 || remaining != want {
			t.Fatalf("monthly purchase/reward benefit was not merged exactly once user%d: count%d remaining%d", user, count, remaining)
		}
	}
}
