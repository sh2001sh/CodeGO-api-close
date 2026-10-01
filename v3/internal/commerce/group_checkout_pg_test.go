//go:build pgintegration

package commerce_test

import (
	"context"
	"crypto/md5" // Verify the real Epay wire protocol in the test fixture.
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
)

type groupSignedProvider struct {
	*commerce.Epay
	calls atomic.Int64
}

func (p *groupSignedProvider) Checkout(ctx context.Context, o commerce.Order, success, cancel string) (commerce.Checkout, error) {
	p.calls.Add(1)
	return p.Epay.Checkout(ctx, o, success, cancel)
}

type groupFixture struct {
	s        *commerce.Service
	pool     *pgxpool.Pool
	market   *marketplace.Service
	mux      *http.ServeMux
	provider *groupSignedProvider
	now      time.Time
}

func newGroupFixture(t *testing.T) *groupFixture {
	t.Helper()
	pool := isolatedPool(t)
	f := &groupFixture{pool: pool, now: time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC)}
	f.provider = &groupSignedProvider{Epay: commerce.NewEpay(commerce.EpayConfig{MerchantID: "test-merchant", Secret: "test-group-secret", BaseURL: "https://cashier.test", NotifyURL: "https://site.test/api/subscription/epay/notify"})}
	poster := ledger.NewPoster(pool)
	f.s = commerce.New(pool, poster, []commerce.PaymentProvider{f.provider}, commerce.Config{Now: func() time.Time { return f.now }, ReturnOrigins: []string{"https://site.test"}})
	f.market = marketplace.New(pool, poster, nil, f.s, f.s, marketplace.Config{Now: func() time.Time { return f.now }})
	f.s.SetGroupCheckoutMarket(f.market)
	f.mux = http.NewServeMux()
	f.s.Register(f.mux, func(r *http.Request) (commerce.Actor, error) {
		id, err := strconv.ParseInt(r.Header.Get("Test-Actor"), 10, 64)
		return commerce.Actor{UserID: id, Role: "user"}, err
	})
	return f
}

func (f *groupFixture) plan(t *testing.T, target int) commerce.Plan {
	t.Helper()
	p, err := f.s.SavePlan(context.Background(), commerce.Plan{Name: "group month", PriceMinor: 1000, Currency: "cny", Credits: 1000, DurationUnit: "month", DurationValue: 1, Enabled: true,
		GroupBuyEnabled: true, GroupBuyTarget: target, GroupBuyLifetimeSeconds: 3600, GroupBuyBonus2: 100, GroupBuyBonus3: 200, GroupBuyBonus5: 300})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *groupFixture) checkout(user int64, fields map[string]any, legacy bool) (commerce.Order, int, string) {
	path := "/api/commerce/orders"
	if legacy {
		path = "/api/subscription/epay/pay"
	} else {
		fields["provider"] = "epay"
	}
	fields["success_url"], fields["cancel_url"] = "https://site.test/success", "https://site.test/cancel"
	body, err := json.Marshal(fields)
	if err != nil {
		return commerce.Order{}, 0, err.Error()
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	req.Header.Set("Test-Actor", strconv.FormatInt(user, 10))
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return commerce.Order{}, rec.Code, rec.Body.String()
	}
	orders, err := f.s.ListOrders(context.Background(), user, 0, 1)
	if err != nil || len(orders) != 1 {
		return commerce.Order{}, 0, fmt.Sprintf("orders=%v error=%v", orders, err)
	}
	return orders[0], rec.Code, rec.Body.String()
}

func groupEpayBody(o commerce.Order) string {
	v := url.Values{"pid": {"test-merchant"}, "trade_status": {"TRADE_SUCCESS"}, "trade_no": {"group-event-" + o.TradeNo}, "out_trade_no": {o.TradeNo}, "money": {fmt.Sprintf("%d.%02d", o.AmountMinor/100, o.AmountMinor%100)}}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+v.Get(k))
	}
	digest := md5.Sum([]byte(strings.Join(parts, "&") + "test-group-secret"))
	v.Set("sign", hex.EncodeToString(digest[:]))
	v.Set("sign_type", "MD5")
	return v.Encode()
}

func (f *groupFixture) pay(o commerce.Order) error {
	return f.s.HandleWebhook(context.Background(), "epay", nil, []byte(groupEpayBody(o)))
}

func (f *groupFixture) room(t *testing.T, orderID int64) int64 {
	t.Helper()
	var id int64
	if err := f.pool.QueryRow(context.Background(), `SELECT group_buy_id FROM v3_marketplace.group_buy_members WHERE order_id=$1`, orderID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestGroupCheckoutNormalAndLegacyPaymentsShareRoomAndBonusBudget(t *testing.T) {
	f := newGroupFixture(t)
	p := f.plan(t, 2)
	first, code, body := f.checkout(1, map[string]any{"plan_id": p.ID}, false)
	if code != 200 || first.PurchaseType != "group_buy" {
		t.Fatalf("normal group-enabled checkout not normalized: order=%+v status=%d body=%s", first, code, body)
	}
	if err := f.pay(first); err != nil {
		t.Fatal(err)
	}
	second, code, body := f.checkout(2, map[string]any{"plan_id": p.ID}, true)
	if code != 200 || second.PurchaseType != "group_buy" || !strings.Contains(body, "form") {
		t.Fatalf("legacy checkout=%+v status=%d body=%s", second, code, body)
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/subscription/epay/notify", strings.NewReader(groupEpayBody(second))))
	if rec.Code != 200 || rec.Body.String() != "success" {
		t.Fatalf("signed legacy callback=%d %s", rec.Code, rec.Body.String())
	}
	if err := f.pay(second); err != nil {
		t.Fatal(err)
	}
	id := f.room(t, first.ID)
	if id != f.room(t, second.ID) {
		t.Fatal("normal payments did not share the source group pool")
	}
	g, err := f.market.GetGroup(context.Background(), id)
	if err != nil || g.CurrentCount != 2 || g.Status != "completed" {
		t.Fatalf("room=%+v error=%v", g, err)
	}
	for _, user := range []int64{1, 2} {
		subs, err := f.s.ListSubscriptions(context.Background(), user)
		if err != nil || len(subs) != 1 || subs[0].Balance != 1100 || subs[0].TotalCredits != 1100 || subs[0].UsedCredits != 0 {
			t.Fatalf("group bonus not spendable in lifetime budget user=%d subs=%+v error=%v", user, subs, err)
		}
	}
	var members, receipts int
	if err = f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM v3_marketplace.group_buy_members),(SELECT count(*) FROM v3_commerce.payment_events)`).Scan(&members, &receipts); err != nil || members != 2 || receipts != 2 {
		t.Fatalf("replay added membership or receipt: members=%d receipts=%d error=%v", members, receipts, err)
	}
}

func TestGroupCheckoutKeepsPaidGroupRulesFrozenAcrossPlanEdits(t *testing.T) {
	f := newGroupFixture(t)
	p := f.plan(t, 2)
	orders := make([]commerce.Order, 2)
	for i := range orders {
		o, code, body := f.checkout(int64(i+1), map[string]any{"plan_id": p.ID}, false)
		if code != 200 {
			t.Fatalf("original quote=%d %s", code, body)
		}
		orders[i] = o
	}
	p.GroupBuyEnabled, p.GroupBuyTarget, p.GroupBuyBonus2, p.PriceMinor, p.Credits = false, 5, 900, 2000, 5000
	if _, err := f.s.SavePlan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	for _, o := range orders {
		if err := f.pay(o); err != nil {
			t.Fatal(err)
		}
	}
	id := f.room(t, orders[0].ID)
	g, err := f.market.GetGroup(context.Background(), id)
	if err != nil || g.TargetCount != 2 || g.BonusAt2Micro != 100 || g.Status != "completed" || f.room(t, orders[1].ID) != id {
		t.Fatalf("plan edit changed paid group contract: group=%+v error=%v", g, err)
	}
	for _, user := range []int64{1, 2} {
		subs, err := f.s.ListSubscriptions(context.Background(), user)
		if err != nil || len(subs) != 1 || subs[0].Balance != 1100 || subs[0].TotalCredits != 1100 {
			t.Fatalf("plan edit changed paid package/bonus: user=%d subs=%+v error=%v", user, subs, err)
		}
	}
}
