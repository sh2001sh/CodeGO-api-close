package commerce

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestEpayCallbackSignsAmountMerchantAndRejectsDuplicateFields(t *testing.T) {
	p := NewEpay(EpayConfig{MerchantID: "10001", Secret: "epay-test-secret", BaseURL: "https://pay.test", NotifyURL: "https://site.test/api/user/epay/notify"})
	checkout, err := p.Checkout(context.Background(), Order{TradeNo: "v3_paid", Kind: "topup", AmountMinor: 1250, Currency: "cny"}, "https://site.test/success", "")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(checkout.URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("money") != "12.50" || u.Query().Get("out_trade_no") != "v3_paid" {
		t.Fatalf("checkout URL: %s", checkout.URL)
	}
	fields := url.Values{"pid": {"10001"}, "trade_no": {"gateway_1"}, "out_trade_no": {"v3_paid"}, "trade_status": {"TRADE_SUCCESS"}, "money": {"12.50"}, "sign_type": {"MD5"}}
	fields.Set("sign", epaySign(fields, p.cfg.Secret))
	e, err := p.Verify(context.Background(), nil, []byte(fields.Encode()))
	if err != nil || e.AmountMinor != 1250 || !e.Paid || e.ID != "gateway_1" {
		t.Fatalf("event=%+v err=%v", e, err)
	}
	for _, invalid := range []string{strings.Replace(fields.Encode(), "12.50", "0.01", 1), fields.Encode() + "&money=0.01", strings.Replace(fields.Encode(), "pid=10001", "pid=10002", 1)} {
		if _, err := p.Verify(context.Background(), nil, []byte(invalid)); err == nil {
			t.Fatalf("invalid callback accepted: %s", invalid)
		}
	}
}

func TestCreemCheckoutAndRawBodySignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ProductID string `json:"product_id"`
			RequestID string `json:"request_id"`
			Price     int64  `json:"custom_price"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.ProductID != "prod_1" || request.RequestID != "v3_1" || request.Price != 1500 || r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("request=%+v", request)
		}
		_, _ = w.Write([]byte(`{"id":"checkout_1","checkout_url":"https://creem.test/pay"}`))
	}))
	defer server.Close()
	p := NewCreem(CreemConfig{APIKey: "test-key", WebhookSecret: "test-secret", ProductID: "prod_1", BaseURL: server.URL, Client: server.Client()})
	checkout, err := p.Checkout(context.Background(), Order{TradeNo: "v3_1", Currency: "usd", AmountMinor: 1500}, "https://site.test/success", "")
	if err != nil || checkout.Reference != "checkout_1" {
		t.Fatalf("checkout=%+v err=%v", checkout, err)
	}
	body := `{"id":"evt_1","eventType":"checkout.completed","object":{"id":"checkout_1","request_id":"v3_1","order":{"status":"paid","amount":1500,"currency":"USD"}}}`
	mac := hmac.New(sha256.New, []byte(p.cfg.WebhookSecret))
	_, _ = mac.Write([]byte(body))
	header := http.Header{}
	header.Set("creem-signature", hex.EncodeToString(mac.Sum(nil)))
	e, err := p.Verify(context.Background(), header, []byte(body))
	if err != nil || e.AmountMinor != 1500 || !e.Paid || e.Currency != "usd" {
		t.Fatalf("event=%+v err=%v", e, err)
	}
	if _, err := p.Verify(context.Background(), header, []byte(strings.Replace(body, "1500", "1", 1))); err == nil {
		t.Fatal("tampered Creem amount accepted")
	}
}

func TestExactMinorAmountParsingRejectsOverflowAndExtraPrecision(t *testing.T) {
	for raw, expected := range map[string]int64{"12": 1200, "12.5": 1250, "12.50": 1250, "0.01": 1} {
		actual, err := parseMinor(raw)
		if err != nil || actual != expected {
			t.Fatalf("%s=%d err=%v", raw, actual, err)
		}
	}
	for _, raw := range []string{"1.001", "-1.00", "1e2", "92233720368547758.08", "NaN", ".50", "1."} {
		if _, err := parseMinor(raw); err == nil {
			t.Fatalf("invalid amount accepted: %s", raw)
		}
	}
}
