//go:build pgintegration

package commerce_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

type legacyFakePayment struct {
	fakePayment
	provider string
}

func (p legacyFakePayment) Name() string { return p.provider }

func TestLegacyCheckoutPreservesCashierURLFieldsAndStablecoinPricing(t *testing.T) {
	pool := isolatedPool(t)
	s := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{
		commerce.NewEpay(commerce.EpayConfig{MerchantID: "merchant", Secret: "test-secret", BaseURL: "https://epay.test", NotifyURL: "https://site.test/notify"}),
		legacyFakePayment{provider: "waffo"}, legacyFakePayment{provider: "nowpayments"},
	}, commerce.Config{ReturnOrigins: []string{"https://site.test"}, ProviderPricing: map[string]commerce.TopupPrice{
		"epay": {Currency: "cny", CreditsPerMinor: 10000}, "nowpayments": {Currency: "usdt", CreditsPerMinor: 1},
	}})
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (commerce.Actor, error) { return commerce.Actor{UserID: 1, Role: "user"}, nil })
	for _, provider := range []string{"epay", "waffo", "nowpayments"} {
		path := "/api/user/" + provider + "/pay"
		if provider == "epay" {
			path = "/api/user/pay"
		}
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"amount":1}`))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var response struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			URL     string `json:"url"`
			Data    struct {
				OrderID    string            `json:"order_id"`
				PaymentURL string            `json:"payment_url"`
				PayURL     string            `json:"pay_url"`
				PaymentID  string            `json:"payment_id"`
				Form       map[string]string `json:"form"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || !response.Success || response.Message != "success" || response.Data.PaymentURL == "" || response.Data.PayURL == "" || response.Data.PaymentID == "" {
			t.Fatalf("legacy %s checkout=%s", provider, w.Body.String())
		}
		if provider == "epay" && (response.URL != "https://epay.test/submit.php" || response.Data.Form["out_trade_no"] != response.Data.OrderID || response.Data.Form["money"] != "1.00" || response.Data.Form["sign"] == "") {
			t.Fatalf("legacy signed epay form=%+v", response)
		}
		o, err := s.GetOrder(r.Context(), 1, response.Data.OrderID)
		if err != nil || o.Credits != 1_000_000 {
			t.Fatalf("legacy %s credits=%d err=%v", provider, o.Credits, err)
		}
		if provider == "nowpayments" && (o.AmountMinor != 1_000_000 || o.Currency != "usdt") {
			t.Fatalf("legacy crypto minor units=%+v", o)
		}
	}
}
