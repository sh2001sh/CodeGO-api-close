package commerce

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func nowPaymentsSignature(secret, canonical string) http.Header {
	mac := hmac.New(sha512.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	header := http.Header{}
	header.Set("x-nowpayments-sig", hex.EncodeToString(mac.Sum(nil)))
	return header
}

func TestNowPaymentsInvoiceCheckoutUsesExactDecimalAndHostedURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var wire struct {
			Amount   json.Number `json:"price_amount"`
			Currency string      `json:"price_currency"`
			PayCoin  string      `json:"pay_currency"`
			Trade    string      `json:"order_id"`
			Notify   string      `json:"ipn_callback_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/invoice" || r.Header.Get("x-api-key") != "test-api-key" || r.Header.Get("Idempotency-Key") != "v3_exact" || wire.Amount.String() != "90071992547409.93" || wire.Currency != "usd" || wire.PayCoin != "usdttrc20" || wire.Trade != "v3_exact" || wire.Notify != "https://site.test/ipn" {
			t.Errorf("invoice request: %+v", wire)
		}
		_, _ = w.Write([]byte(`{"id":9007199254740993,"invoice_url":"https://nowpayments.test/invoice/1"}`))
	}))
	defer server.Close()
	p := NewNowPayments(NowPaymentsConfig{APIKey: "test-api-key", NotifyURL: "https://site.test/ipn", BaseURL: server.URL, Client: server.Client()})
	got, err := p.Checkout(context.Background(), Order{TradeNo: "v3_exact", AmountMinor: 9007199254740993, Currency: "usd"}, "https://site.test/done", "https://site.test/cancel")
	if err != nil || got.Reference != "9007199254740993" || got.URL == "" {
		t.Fatalf("checkout=%+v err=%v", got, err)
	}
	if _, err = p.Checkout(context.Background(), Order{TradeNo: "v3_wrong", Currency: "cny", AmountMinor: 100}, "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong currency accepted: %v", err)
	}
}

func TestNowPaymentsIPNCanonicalSignatureAndExactUnderpayment(t *testing.T) {
	p := NewNowPayments(NowPaymentsConfig{IPNSecret: "test-ipn-secret"})
	// Independent canonical fixture also exercises nested object sorting and
	// JSON.stringify-compatible angle brackets and ampersands.
	canonical := `{"actually_paid":"12.345678901234567890","invoice_id":9007199254740993,"metadata":{"a":"<x>&","z":1},"order_id":"v3_paid","pay_amount":"12.345678901234567890","payment_id":9007199254740995,"payment_status":"finished","price_amount":12.500000,"price_currency":"usd"}`
	body := `{"price_currency":"usd","price_amount":12.500000,"payment_status":"finished","payment_id":9007199254740995,"pay_amount":"12.345678901234567890","order_id":"v3_paid","metadata":{"z":1,"a":"<x>&"},"invoice_id":9007199254740993,"actually_paid":"12.345678901234567890"}`
	header := nowPaymentsSignature(p.cfg.IPNSecret, canonical)
	event, err := p.Verify(context.Background(), header, []byte(body))
	if err != nil || event.Reference != "9007199254740993" || event.ID != "9007199254740995:finished" || event.AmountMinor != 1250 || !event.Paid {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	if _, err = p.Verify(context.Background(), header, []byte(strings.Replace(body, "12.500000", "1.00", 1))); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered amount accepted: %v", err)
	}
	for _, tc := range []struct {
		name, old, replacement string
		want                   error
	}{
		{"underpaid", `"actually_paid":"12.345678901234567890"`, `"actually_paid":"12.345678901234567889"`, ErrPaymentMismatch},
		{"extra precision", `"price_amount":12.500000`, `"price_amount":12.500001`, ErrInvalid},
		{"wrong currency", `"price_currency":"usd"`, `"price_currency":"cny"`, ErrInvalid},
		{"overflow", `"price_amount":12.500000`, `"price_amount":92233720368547758.08`, ErrInvalid},
		{"missing reference", `"invoice_id":9007199254740993`, `"invoice_id":null`, ErrInvalid},
		{"partial", `"payment_status":"finished"`, `"payment_status":"partially_paid"`, ErrIgnoredEvent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modified := strings.Replace(canonical, tc.old, tc.replacement, 1)
			_, err := p.Verify(context.Background(), nowPaymentsSignature(p.cfg.IPNSecret, modified), []byte(modified))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
	if _, err = p.Verify(context.Background(), header, []byte(body+` {}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("trailing document accepted: %v", err)
	}
}

func TestNowPaymentsStablecoinPriceKeepsSixDecimalMinorUnits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Amount json.Number `json:"price_amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Amount.String() != "1.234567" {
			t.Errorf("stablecoin price=%s", request.Amount)
		}
		_, _ = w.Write([]byte(`{"id":7,"invoice_url":"https://nowpayments.test/invoice/7"}`))
	}))
	defer server.Close()
	p := NewNowPayments(NowPaymentsConfig{APIKey: "test-key", IPNSecret: "test-secret", Currency: "usdt", NotifyURL: "https://site.test/ipn", BaseURL: server.URL, Client: server.Client()})
	if _, err := p.Checkout(context.Background(), Order{TradeNo: "v3_usdt", AmountMinor: 1_234_567, Currency: "usdt"}, "", ""); err != nil {
		t.Fatal(err)
	}
	body := `{"actually_paid":"1.234567","invoice_id":7,"order_id":"v3_usdt","pay_amount":"1.234567","payment_id":8,"payment_status":"finished","price_amount":1.234567,"price_currency":"usdt"}`
	event, err := p.Verify(context.Background(), nowPaymentsSignature(p.cfg.IPNSecret, body), []byte(body))
	if err != nil || event.AmountMinor != 1_234_567 {
		t.Fatalf("stablecoin event=%+v err=%v", event, err)
	}
	invalid := strings.ReplaceAll(body, "1.234567", "1.2345678")
	if _, err = p.Verify(context.Background(), nowPaymentsSignature(p.cfg.IPNSecret, invalid), []byte(invalid)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("excess stablecoin precision accepted: %v", err)
	}
}

func TestNowPaymentsCheckoutProviderFailureIsReportedWithoutBody(t *testing.T) {
	for _, reply := range []struct {
		status int
		body   string
	}{{502, "private-account@example.test"}, {200, `{"id":123}`}, {200, `{"id":"bad","invoice_url":"https://pay.test"}`}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(reply.status)
			_, _ = w.Write([]byte(reply.body))
		}))
		p := NewNowPayments(NowPaymentsConfig{APIKey: "test-key", NotifyURL: "https://site.test/ipn", BaseURL: server.URL, Client: server.Client()})
		_, err := p.Checkout(context.Background(), Order{TradeNo: "v3_test", Currency: "usd", AmountMinor: 100}, "", "")
		server.Close()
		if err == nil || strings.Contains(err.Error(), "private-account") {
			t.Fatalf("provider failure err=%v", err)
		}
	}
}
