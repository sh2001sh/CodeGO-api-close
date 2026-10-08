//go:build pgintegration

package commerce_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestCashierChoiceIsSavedWithImmutableOrder(t *testing.T) {
	pool := isolatedPool(t)
	p := commerce.NewEpay(commerce.EpayConfig{MerchantID: "merchant", Secret: "test-secret", BaseURL: "https://cashier.test", NotifyURL: "https://site.test/notify"})
	s := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{p}, commerce.Config{
		ReturnOrigins: []string{"https://site.test"}, ProviderPricing: map[string]commerce.TopupPrice{"epay": {Currency: "cny", CreditsPerMinor: 10000}},
	})
	in := commerce.CreateOrder{UserID: 1, Provider: "epay", AmountMinor: 123, SuccessURL: "https://site.test/success", CancelURL: "https://site.test/cancel", Selection: commerce.CheckoutSelection{PaymentMethod: "wxpay"}}
	o, err := s.Create(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.GetOrder(context.Background(), 1, o.TradeNo)
	if err != nil || saved.Selection != in.Selection || saved.AmountMinor != 123 || saved.Credits != 1230000 {
		t.Fatalf("selection snapshot=%+v err=%v", saved, err)
	}
	u, err := url.Parse(saved.PaymentURL)
	if err != nil || u.Query().Get("type") != "wxpay" {
		t.Fatalf("selected checkout=%s err=%v", saved.PaymentURL, err)
	}
	in.Selection = commerce.CheckoutSelection{PayCurrency: "btc"}
	if _, err := s.Create(context.Background(), in); err == nil {
		t.Fatal("cross-provider choice accepted")
	}
}

func TestEpayPackageAndFuelHTTPPreserveConfiguredCashier(t *testing.T) {
	pool := isolatedPool(t)
	p := commerce.NewEpay(commerce.EpayConfig{MerchantID: "merchant", Secret: "test-secret", BaseURL: "https://cashier.test", NotifyURL: "https://site.test/notify", PaymentTypes: []string{"wxpay"}})
	s := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{p}, commerce.Config{ReturnOrigins: []string{"https://site.test"}, ProviderPricing: map[string]commerce.TopupPrice{"epay": {Currency: "cny", CreditsPerMinor: 10000}}})
	plan, err := s.SavePlan(context.Background(), commerce.Plan{Name: "cashier fixture", Currency: "cny", PriceMinor: 1000, Credits: 10000000, DurationUnit: "month", DurationValue: 1, Enabled: true, PlanType: "monthly", FuelEnabled: true, FuelUnitPriceMicro: 1000000, FuelMinCredits: 1000000, FuelCreditStep: 1000000})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (commerce.Actor, error) { return commerce.Actor{UserID: 1, Role: "user"}, nil })
	post := func(path, body string) commerce.Order {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		var response struct {
			Success bool           `json:"success"`
			Data    commerce.Order `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 || !response.Success {
			t.Fatalf("fixture checkout failed: status=%d", w.Code)
		}
		u, err := url.Parse(response.Data.PaymentURL)
		if err != nil || u.Query().Get("type") != "wxpay" || response.Data.Selection.PaymentMethod != "wxpay" {
			t.Fatal("HTTP selected cashier lost before signed checkout")
		}
		return response.Data
	}
	o := post("/api/packages/purchase", fmt.Sprintf(`{"plan_id":%d,"provider":"epay","payment_method":"wxpay","success_url":"https://site.test/orders","cancel_url":"https://site.test/wallet"}`, plan.ID))
	if err := s.Fulfill(context.Background(), "epay", commerce.PaymentEvent{ID: "fixture-paid", TradeNo: o.TradeNo, Reference: o.TradeNo, AmountMinor: o.AmountMinor, Currency: "cny", Paid: true}); err != nil {
		t.Fatal(err)
	}
	sub := onlySubscription(t, s)
	post("/api/subscription/fuel/purchase", fmt.Sprintf(`{"subscription_id":%d,"credits":1000000,"provider":"epay","payment_method":"wxpay","success_url":"https://site.test/orders","cancel_url":"https://site.test/wallet"}`, sub.ID))
	var before, after int64
	if err = pool.QueryRow(context.Background(), `SELECT count(*) FROM v3_commerce.orders`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/commerce/orders", strings.NewReader(`{"provider":"epay","amount_minor":100,"checkout_selection":{"payment_method":"alipay"},"success_url":"https://site.test/orders","cancel_url":"https://site.test/wallet"}`)))
	if err = pool.QueryRow(context.Background(), `SELECT count(*) FROM v3_commerce.orders`).Scan(&after); err != nil || w.Code != 400 || after != before {
		t.Fatal("disabled cashier created an order or passed authorization")
	}
}
