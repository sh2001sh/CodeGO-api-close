//go:build pgintegration

package commerce_test

import (
	"context"
	"crypto/md5" // Epay's wire protocol fixes the callback digest to MD5.
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestCashBoxCheckoutFailureAndCancellationRetainLateVerifiedPayment(t *testing.T) {
	s, m, pool, p := cashBoxFixture(t)
	broken := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{commerce.NewEpay(commerce.EpayConfig{})},
		commerce.Config{ReturnOrigins: []string{"https://site.test"}})
	broken.SetCashBoxMarket(m)
	if _, err := broken.CreateCashBox(context.Background(), commerce.CreateCashBox{UserID: 1, PoolID: p.ID, Quantity: 1,
		Provider: "epay", SuccessURL: "https://site.test/success", CancelURL: "https://site.test/cancel"}); !errors.Is(err, commerce.ErrProviderUnavailable) {
		t.Fatalf("unconfigured checkout succeeded: %v", err)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_commerce.orders WHERE state='failed'`); n != 1 {
		t.Fatalf("failure not durable: %d", n)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_orders WHERE status='expired'`); n != 1 {
		t.Fatalf("failure holds purchase limit: %d", n)
	}
	o := cashBoxCreate(t, s, p.ID, 2)
	if err := s.CancelCashBox(context.Background(), 2, o.TradeNo); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("cross-user cancellation accepted: %v", err)
	}
	for range 2 {
		if err := s.CancelCashBox(context.Background(), 1, o.TradeNo); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Fulfill(context.Background(), "epay", payment(o)); err != nil {
		t.Fatalf("late verified paid cancellation failed: %v", err)
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_items WHERE status='available'`); n != 2 {
		t.Fatalf("late payment inventory=%d", n)
	}
	o = cashBoxCreate(t, s, p.ID, 1)
	if _, err := pool.Exec(context.Background(), `UPDATE v3_commerce.orders SET state='expired' WHERE id=$1`, o.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.RecoverCashBoxOrders(context.Background(), 100); err != nil || n != 1 {
		t.Fatalf("expiry recovery count=%d err=%v", n, err)
	}
	if err := s.Fulfill(context.Background(), "epay", payment(o)); err != nil {
		t.Fatalf("late verified paid expiry failed: %v", err)
	}
}

func TestCashBoxQuoteUsesExactCashPriceAndAdmissionRollsBackBothOrders(t *testing.T) {
	s, m, pool, p := cashBoxFixture(t)
	for _, count := range []int{0, -1, 101} {
		if _, err := s.QuoteCashBox(context.Background(), 1, p.ID, count); !errors.Is(err, commerce.ErrInvalid) {
			t.Fatalf("invalid quantity %d accepted: %v", count, err)
		}
	}
	p.Price = 2500001
	if _, err := m.SavePool(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.QuoteCashBox(context.Background(), 1, p.ID, 1); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("price silently rounded to cents: %v", err)
	}
	p.Price, p.DailyLimit = 2500000, 1
	if _, err := m.SavePool(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	q, err := s.QuoteCashBox(context.Background(), 1, 0, 1)
	if err != nil || q.AmountMinor != 250 || q.PoolID != p.ID {
		t.Fatalf("default cash quote=%+v err=%v", q, err)
	}
	cashBoxCreate(t, s, p.ID, 1)
	if _, err := s.CreateCashBox(context.Background(), commerce.CreateCashBox{UserID: 1, PoolID: p.ID, Quantity: 1,
		Provider: "epay", SuccessURL: "https://site.test/success", CancelURL: "https://site.test/cancel"}); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("daily limit not enforced: %v", err)
	}
	for _, sql := range []string{`SELECT count(*) FROM v3_commerce.orders`, `SELECT count(*) FROM v3_marketplace.blind_box_orders`} {
		if n := cashBoxCount(t, pool, sql); n != 1 {
			t.Fatalf("admission rollback left order=%d: %s", n, sql)
		}
	}
	p.Scope = "credits"
	if _, err := m.SavePool(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.QuoteCashBox(context.Background(), 1, p.ID, 1); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("credit pool conflated with CNY cash: %v", err)
	}
}

func cashBoxSign(values url.Values) string {
	var keys []string
	for key := range values {
		if key != "sign" && key != "sign_type" && key != "hash" && values.Get(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var pairs []string
	for _, key := range keys {
		pairs = append(pairs, key+"="+values.Get(key))
	}
	digest := md5.Sum([]byte(strings.Join(pairs, "&") + "test-only-secret"))
	return hex.EncodeToString(digest[:])
}

func TestCashBoxLegacyHTTPVerifiesNotifyAndBrowserReturnCannotGrant(t *testing.T) {
	s, _, pool, _ := cashBoxFixture(t)
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (commerce.Actor, error) { return commerce.Actor{UserID: 1, Role: "user"}, nil })
	quote := httptest.NewRecorder()
	mux.ServeHTTP(quote, httptest.NewRequest("POST", "/api/blind-box/amount", strings.NewReader(`{"quantity":2}`)))
	if quote.Code != 200 || !strings.Contains(quote.Body.String(), `"data":"5.00"`) {
		t.Fatalf("legacy amount quote: %d %s", quote.Code, quote.Body.String())
	}
	checkout := httptest.NewRecorder()
	mux.ServeHTTP(checkout, httptest.NewRequest("POST", "/api/blind-box/pay", strings.NewReader(`{"quantity":2,"payment_method":"wxpay"}`)))
	var body struct {
		Message string
		URL     string
		Data    struct {
			OrderID string            `json:"order_id"`
			Form    map[string]string `json:"form"`
		}
	}
	if err := json.Unmarshal(checkout.Body.Bytes(), &body); err != nil || checkout.Code != 200 || body.Message != "success" || body.URL != "https://cashier.test/submit.php" || body.Data.Form["type"] != "wxpay" || body.Data.Form["money"] != "5.00" {
		t.Fatalf("legacy checkout contract: %d %s err=%v", checkout.Code, checkout.Body.String(), err)
	}
	back := httptest.NewRecorder()
	mux.ServeHTTP(back, httptest.NewRequest("GET", "/api/blind-box/epay/return?trade_no="+body.Data.OrderID, nil))
	if back.Code != http.StatusSeeOther || back.Header().Get("Location") != "https://site.test/blind-box?pay=pending" {
		t.Fatalf("browser return: %d %s", back.Code, back.Header().Get("Location"))
	}
	values := url.Values{"pid": {"merchant"}, "out_trade_no": {body.Data.OrderID}, "trade_no": {"external-event"},
		"trade_status": {"TRADE_SUCCESS"}, "money": {"5.00"}, "type": {"wxpay"}, "sign_type": {"MD5"}}
	values.Set("sign", "tampered")
	bad := httptest.NewRecorder()
	mux.ServeHTTP(bad, httptest.NewRequest("GET", "/api/blind-box/epay/notify?"+values.Encode(), nil))
	if bad.Code == 200 || cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_items`) != 0 {
		t.Fatalf("unverified callback granted inventory: %d %s", bad.Code, bad.Body.String())
	}
	values.Set("sign", cashBoxSign(values))
	for range 2 {
		good := httptest.NewRecorder()
		mux.ServeHTTP(good, httptest.NewRequest("GET", "/api/blind-box/epay/notify?"+values.Encode(), nil))
		if good.Code != 200 || good.Body.String() != "success" {
			t.Fatalf("verified callback: %d %s", good.Code, good.Body.String())
		}
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_items`); n != 2 {
		t.Fatalf("notify replay inventory=%d", n)
	}
}

func TestCashBoxXunhuPreservesSignedCashierAndQRCodeAndVerifiedInventory(t *testing.T) {
	_, m, pool, _ := cashBoxFixture(t)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("total_fee") != "5.00" || r.Form.Get("hash") != cashBoxSign(r.Form) {
			t.Errorf("unsigned or incorrect cash request: %s", r.Form.Encode())
		}
		v := url.Values{"errcode": {"0"}, "trade_order_id": {r.Form.Get("trade_order_id")},
			"url": {"https://cashier.test/xunhu"}, "url_qrcode": {"https://cashier.test/qr.png"}}
		out := map[string]string{"hash": cashBoxSign(v)}
		for k := range v {
			out[k] = v.Get(k)
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer gateway.Close()
	s := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{commerce.NewXunhu(commerce.XunhuConfig{
		AppID: "merchant", Secret: "test-only-secret", Gateway: gateway.URL, Client: gateway.Client(), NotifyURL: "https://site.test/api/blind-box/xunhu/notify"})},
		commerce.Config{ReturnOrigins: []string{"https://site.test"}})
	s.SetCashBoxMarket(m)
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (commerce.Actor, error) { return commerce.Actor{UserID: 1, Role: "user"}, nil })
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/blind-box/pay", strings.NewReader(`{"quantity":2,"payment_method":"xunhu"}`)))
	var reply struct {
		Data struct {
			OrderID string `json:"order_id"`
			PayURL  string `json:"pay_url"`
			QRURL   string `json:"qrcode_url"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || w.Code != 200 || reply.Data.PayURL != "https://cashier.test/xunhu" || reply.Data.QRURL != "https://cashier.test/qr.png" {
		t.Fatalf("real signed Xunhu cashier/QR lost: %d %s err=%v", w.Code, w.Body.String(), err)
	}
	v := url.Values{"appid": {"merchant"}, "trade_order_id": {reply.Data.OrderID}, "total_fee": {"5.00"}, "status": {"OD"}}
	v.Set("hash", cashBoxSign(v))
	for range 2 {
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/blind-box/xunhu/notify?"+v.Encode(), nil))
		if w.Code != 200 || w.Body.String() != "success" {
			t.Fatalf("signed Xunhu fulfillment failed: %d %s", w.Code, w.Body.String())
		}
	}
	if n := cashBoxCount(t, pool, `SELECT count(*) FROM v3_marketplace.blind_box_items`); n != 2 {
		t.Fatalf("Xunhu replay inventory=%d", n)
	}
}
