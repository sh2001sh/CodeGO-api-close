//go:build pgintegration

package commerce_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

// Regression: legacy Creem requests select a product without a browser price.
// Even an explicitly supplied amount cannot replace the server's product quote.
func TestProductOrderUsesServerQuoteAndRejectsUnknownProduct(t *testing.T) {
	pool := isolatedPool(t)
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ProductID string `json:"product_id"`
			Price     int64  `json:"custom_price"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input.ProductID != "prod_server" || input.Price != 2500 {
			t.Errorf("upstream received untrusted product price: %+v", input)
		}
		_, _ = w.Write([]byte(`{"id":"checkout_product","checkout_url":"https://creem.test/pay"}`))
	}))
	defer server.Close()
	provider := commerce.NewCreem(commerce.CreemConfig{APIKey: "test-key", BaseURL: server.URL, Client: server.Client(),
		Products: map[string]commerce.ProductQuote{"prod_server": {AmountMinor: 2500, Credits: 35_000_000}}})
	s := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{provider}, commerce.Config{ReturnOrigins: []string{"https://site.test"}})
	input := commerce.CreateOrder{UserID: 1, Provider: "creem", ProductID: "prod_server", AmountMinor: 1,
		SuccessURL: "https://site.test/success", CancelURL: "https://site.test/cancel"}
	o, err := s.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if o.ProductID != "prod_server" || o.AmountMinor != 2500 || o.Credits != 35_000_000 || o.Currency != "usd" {
		t.Fatalf("untrusted product quote persisted: %+v", o)
	}
	if err = s.Fulfill(ctx, "creem", payment(o)); err != nil {
		t.Fatal(err)
	}
	var balance int64
	if err = pool.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=1 AND kind='wallet'`).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if balance != 35_000_000 {
		t.Fatalf("product grant=%d, expected server quote", balance)
	}
	input.ProductID = "prod_unknown"
	if _, err = s.Create(ctx, input); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("unknown product accepted: %v", err)
	}
	input.ProductID, input.PlanID = "prod_server", 1
	if _, err = s.Create(ctx, input); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("ambiguous subscription/product accepted: %v", err)
	}
	var count int64
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.orders`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("invalid orders persisted: count=%d err=%v", count, err)
	}
}

func TestCryptoPriceCurrencyPersistsWithoutFloatConversion(t *testing.T) {
	pool := isolatedPool(t)
	ctx := context.Background()
	s := commerce.New(pool, ledger.NewPoster(pool), []commerce.PaymentProvider{fakePayment{}}, commerce.Config{
		ReturnOrigins: []string{"https://site.test"}, ProviderPricing: map[string]commerce.TopupPrice{
			"test": {Currency: "usdt", CreditsPerMinor: 12000},
		}})
	o := create(t, s, 0)
	if o.Currency != "usdt" || o.Credits != 14_400_000 {
		t.Fatalf("crypto price snapshot=%+v", o)
	}
	if err := s.Fulfill(ctx, "test", payment(o)); err != nil {
		t.Fatal(err)
	}
}
