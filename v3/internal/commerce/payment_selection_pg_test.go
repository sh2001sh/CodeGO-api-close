//go:build pgintegration

package commerce_test

import (
	"context"
	"net/url"
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
