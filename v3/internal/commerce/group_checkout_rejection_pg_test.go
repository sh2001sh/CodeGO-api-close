//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

func TestGroupCheckoutRejectsInvalidRoomAndMembershipBeforeCashier(t *testing.T) {
	f := newGroupFixture(t)
	p := f.plan(t, 3)
	first, code, body := f.checkout(1, map[string]any{"plan_id": p.ID}, false)
	if code != 200 {
		t.Fatalf("first checkout=%d %s", code, body)
	}
	if err := f.pay(first); err != nil {
		t.Fatal(err)
	}
	id := f.room(t, first.ID)
	other := f.plan(t, 3)
	day, err := f.s.SavePlan(context.Background(), commerce.Plan{Name: "day pass", Currency: "cny", PriceMinor: 100, Credits: 100, Enabled: true, DurationUnit: "day", DurationValue: 1, GroupBuyEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	before := f.provider.calls.Load()
	cases := []struct {
		user   int64
		fields map[string]any
	}{
		{1, map[string]any{"plan_id": p.ID}},
		{2, map[string]any{"plan_id": p.ID, "purchase_type": "join_group", "group_buy_id": 99999}},
		{2, map[string]any{"plan_id": other.ID, "purchase_type": "join_group", "group_buy_id": id}},
		{2, map[string]any{"plan_id": day.ID, "purchase_type": "group_buy"}},
		{2, map[string]any{"plan_id": p.ID, "purchase_type": "join_group", "group_buy_id": 0}},
	}
	for i, tc := range cases {
		if _, status, text := f.checkout(tc.user, tc.fields, i%2 == 0); status == 200 {
			t.Fatalf("invalid case%d reached checkout: %s", i, text)
		}
	}
	f.now = f.now.Add(2 * time.Hour)
	if _, status, text := f.checkout(2, map[string]any{"plan_id": p.ID, "purchase_type": "join_group", "group_buy_id": id}, false); status == 200 {
		t.Fatalf("expired room reached cashier: %s", text)
	}
	if calls := f.provider.calls.Load(); calls != before {
		t.Fatalf("rejected request reached provider: before=%d after=%d", before, calls)
	}
	var orders int
	if err = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM v3_commerce.orders`).Scan(&orders); err != nil || orders != 1 {
		t.Fatalf("invalid checkout persisted orders=%d error=%v", orders, err)
	}
}

func TestGroupCheckoutEightPaymentsCompeteForOneSlotAndKeepVerifiedReviews(t *testing.T) {
	f := newGroupFixture(t)
	ctx := context.Background()
	p := f.plan(t, 2)
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) SELECT n,'group-'||n FROM generate_series(3,9) n`); err != nil {
		t.Fatal(err)
	}
	founder, code, body := f.checkout(1, map[string]any{"plan_id": p.ID}, false)
	if code != 200 {
		t.Fatalf("founder=%d %s", code, body)
	}
	if err := f.pay(founder); err != nil {
		t.Fatal(err)
	}
	id := f.room(t, founder.ID)
	orders := make([]commerce.Order, 8)
	for i := range orders {
		o, status, text := f.checkout(int64(i+2), map[string]any{"plan_id": p.ID, "purchase_type": "join_group", "group_buy_id": id}, false)
		if status != 200 {
			t.Fatalf("pending slot checkout=%d %s", status, text)
		}
		orders[i] = o
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(orders))
	for _, o := range orders {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- f.pay(o) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, o := range orders {
		if err := f.pay(o); err != nil {
			t.Fatal(err)
		}
	}
	var members, subscriptions, receipts, reviews int
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_marketplace.group_buy_members),(SELECT count(*) FROM v3_commerce.subscriptions),
	 (SELECT count(*) FROM v3_commerce.payment_events),(SELECT count(*) FROM v3_commerce.package_payment_reviews)`).Scan(&members, &subscriptions, &receipts, &reviews); err != nil || members != 2 || subscriptions != 2 || receipts != 9 || reviews != 7 {
		t.Fatalf("slot conflict lost money or overgranted: members=%d subs=%d receipts=%d reviews=%d error=%v", members, subscriptions, receipts, reviews, err)
	}
	for _, order := range orders {
		o, err := f.s.GetOrder(ctx, order.UserID, order.TradeNo)
		if err != nil || o.State != "paid" {
			t.Fatalf("signed paid record lost: order=%+v error=%v", o, err)
		}
		if o.FulfillmentState == "requires_review" {
			if err = f.s.ConfirmRefund(ctx, "epay", o.TradeNo, "group-full-refund"); err != nil {
				t.Fatal(err)
			}
			if err = f.s.ResolvePackagePaymentReview(ctx, o.ID); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
}

type groupFailureMarket struct{ market *marketplace.Service }

func (m groupFailureMarket) CreateGroupTx(ctx context.Context, tx pgx.Tx, user, order int64) (marketplace.Group, error) {
	g, err := m.market.CreateGroupTx(ctx, tx, user, order)
	if err != nil {
		return g, err
	}
	return g, errors.New("injected group transaction failure")
}
func (m groupFailureMarket) JoinGroupTx(ctx context.Context, tx pgx.Tx, user, group, order int64) (marketplace.Group, error) {
	return m.market.JoinGroupTx(ctx, tx, user, group, order)
}

func TestGroupCheckoutAdmissionFailureRollsBackPaymentGrantAndMembership(t *testing.T) {
	f := newGroupFixture(t)
	p := f.plan(t, 2)
	o, code, body := f.checkout(1, map[string]any{"plan_id": p.ID}, false)
	if code != 200 {
		t.Fatalf("checkout=%d %s", code, body)
	}
	f.s.SetGroupCheckoutMarket(groupFailureMarket{market: f.market})
	if err := f.pay(o); err == nil {
		t.Fatal("injected atomic enrollment failure acknowledged payment")
	}
	var grants, receipts, groups int
	if err := f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM v3_billing.ledger_entries),(SELECT count(*) FROM v3_commerce.payment_events),(SELECT count(*) FROM v3_marketplace.group_buys)`).Scan(&grants, &receipts, &groups); err != nil || grants != 0 || receipts != 0 || groups != 0 {
		t.Fatalf("rollback leaked grant/receipt/group: %d %d %d error=%v", grants, receipts, groups, err)
	}
	saved, err := f.s.GetOrder(context.Background(), 1, o.TradeNo)
	if err != nil || saved.State != "created" {
		t.Fatalf("rolled-back order=%+v error=%v", saved, err)
	}
	bad, err := urlValuesTampered(groupEpayBody(o))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.HandleWebhook(context.Background(), "epay", nil, []byte(bad)); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("unsigned amount mutation accepted: %v", err)
	}
	f.s.SetGroupCheckoutMarket(f.market)
	if err = f.pay(o); err != nil {
		t.Fatal(err)
	}
}

func urlValuesTampered(body string) (string, error) {
	// Changing a signed amount while retaining the signature must fail verification.
	parts := strings.Split(body, "&")
	for i, part := range parts {
		if strings.HasPrefix(part, "money=") {
			parts[i] = "money=999.00"
			return strings.Join(parts, "&"), nil
		}
	}
	return "", fmt.Errorf("signed fixture has no money field")
}
