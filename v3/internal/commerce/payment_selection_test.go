package commerce

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

func TestCashierSelectionsAreRequestScopedAndSigned(t *testing.T) {
	p := NewEpay(EpayConfig{MerchantID: "merchant", Secret: "test-secret", BaseURL: "https://cashier.test", NotifyURL: "https://site.test/notify"})
	s := New(nil, nil, []PaymentProvider{p}, Config{})
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Go(func() {
			method := "alipay"
			if i%2 == 0 {
				method = "wxpay"
			}
			o := Order{Provider: "epay", Currency: "cny", AmountMinor: 123, TradeNo: "trade", Selection: CheckoutSelection{PaymentMethod: method}}
			out, err := s.checkout(context.Background(), o, "https://site.test/success", "")
			if err != nil {
				t.Error(err)
				return
			}
			u, err := url.Parse(out.URL)
			if err != nil {
				t.Error(err)
				return
			}
			fields := u.Query()
			if fields.Get("type") != method || fields.Get("money") != "1.23" || fields.Get("sign") != epaySign(fields, p.cfg.Secret) {
				t.Errorf("wrong selected cashier or signature: %s", out.URL)
			}
		})
	}
	wg.Wait()
	if p.cfg.PaymentType != "alipay" {
		t.Fatal("shared provider mutated")
	}
}

func TestBlockchainSelectionKeepsServerPriceCurrency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var fields map[string]any
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			t.Error(err)
		}
		if fields["pay_currency"] != "btc" || fields["price_currency"] != "usdt" || fields["price_amount"] != 1.234567 {
			t.Errorf("buyer changed price or selection lost: %+v", fields)
		}
		_, _ = w.Write([]byte(`{"id":123,"invoice_url":"https://cashier.test/invoice"}`))
	}))
	defer server.Close()
	p := NewNowPayments(NowPaymentsConfig{APIKey: "test-key", NotifyURL: "https://site.test/notify", Currency: "usdt", BaseURL: server.URL, Client: server.Client()})
	s := New(nil, nil, []PaymentProvider{p}, Config{})
	_, err := s.checkout(context.Background(), Order{Provider: "nowpayments", Currency: "usdt", AmountMinor: 1234567, TradeNo: "trade", Selection: CheckoutSelection{PayCurrency: "btc"}}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.cfg.PayCurrency != "usdttrc20" {
		t.Fatal("shared blockchain setting changed")
	}
}

func TestCheckoutSelectionRejectsCrossProviderAndInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		provider string
		value    CheckoutSelection
	}{
		{"epay", CheckoutSelection{PayCurrency: "btc"}},
		{"stripe", CheckoutSelection{PaymentMethod: "alipay"}},
		{"nowpayments", CheckoutSelection{PayCurrency: "btc\r\n"}},
		{"waffo", CheckoutSelection{PayMethodName: "visa\x00"}},
		{"creem", CheckoutSelection{PayMethodType: "CARD"}},
	} {
		if tc.value.validFor(tc.provider) {
			t.Fatalf("invalid selection accepted: %+v", tc)
		}
	}
	if !(CheckoutSelection{PayMethodType: "CARD", PayMethodName: "VISA"}).validFor("waffo") {
		t.Fatal("valid Waffo choice rejected")
	}
}
