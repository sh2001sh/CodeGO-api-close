package boot

import (
	"context"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestPaymentSettingsRejectUnsafePricingAndPartialConfiguration(t *testing.T) {
	for _, value := range []string{
		`null`,
		`[] []`,
		`[{"provider":"epay","currency":"usd","credits_per_minor":100,"merchant_id":"m","secret":"s","base_url":"https://pay.test"}]`,
		`[{"provider":"stripe","currency":"usd","credits_per_minor":0,"api_key":"sensitive-value","webhook_secret":"signed"}]`,
		`[{"provider":"stripe","currency":"usd","credits_per_minor":100,"api_key":"sensitive-value"}]`,
		`[{"provider":"stripe","currency":"usd","credits_per_minor":100,"api_key":"sensitive-value","webhook_secret":"signed","unknown_field":true}]`,
	} {
		_, err := parsePaymentSettings(value)
		if err == nil || strings.Contains(err.Error(), "sensitive-value") {
			t.Fatalf("unsafe config accepted or secret exposed: %v", err)
		}
	}
}

func TestPaymentProvidersAndPricingAreWired(t *testing.T) {
	clearPaymentEnvironment(t)
	t.Setenv("V3_PAYMENT_PROVIDERS", `[
		{"provider":"stripe","currency":"usd","credits_per_minor":100,"api_key":"test","webhook_secret":"test"},
		{"provider":"epay","currency":"cny","credits_per_minor":20,"merchant_id":"test","secret":"test","base_url":"https://pay.test"},
		{"provider":"creem","currency":"usd","credits_per_minor":100,"api_key":"test","webhook_secret":"test","product_id":"test"},
		{"provider":"xunhu","currency":"cny","credits_per_minor":20,"app_id":"test","secret":"test"},
		{"provider":"nowpayments","currency":"usd","credits_per_minor":100,"api_key":"test","ipn_secret":"test"},
		{"provider":"waffo","currency":"usd","credits_per_minor":100,"api_key":"test","merchant_id":"test","private_key":"test","public_key":"test"},
		{"provider":"waffo_pancake","currency":"usd","credits_per_minor":100,"merchant_id":"test","private_key":"test","public_key":"test","store_id":"test","product_id":"test"}
	]`)
	providers, pricing, refunds, err := LoadPayments(nil, "https://control.test")
	if err != nil || len(providers) != 7 || len(pricing) != 7 || pricing["epay"].Currency != "cny" {
		t.Fatalf("providers=%d pricing=%+v err=%v", len(providers), pricing, err)
	}
	if refunds != nil {
		t.Fatal("generic Epay configuration enabled JianPay refunds")
	}
	t.Setenv("V3_STRIPE_SECRET_KEY", "duplicate")
	t.Setenv("V3_STRIPE_WEBHOOK_SECRET", "test")
	if _, _, _, err := LoadPayments(nil, "https://control.test"); err == nil {
		t.Fatal("duplicate Stripe configuration accepted")
	}
}

func clearPaymentEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"V3_PAYMENT_PROVIDERS", "V3_STRIPE_SECRET_KEY", "V3_STRIPE_WEBHOOK_SECRET", "V3_PAYMENT_CURRENCY", "V3_TOPUP_CREDITS_PER_MINOR"} {
		t.Setenv(name, "")
	}
}

func TestSharedPaymentsFallbackAndRefundOptIn(t *testing.T) {
	clearPaymentEnvironment(t)
	t.Setenv("V3_STRIPE_SECRET_KEY", "sensitive-value")
	if _, _, _, err := LoadPayments(nil, "https://control.test"); err == nil || strings.Contains(err.Error(), "sensitive-value") {
		t.Fatalf("partial/unsafe fallback: %v", err)
	}
	t.Setenv("V3_STRIPE_WEBHOOK_SECRET", "test")
	t.Setenv("V3_TOPUP_CREDITS_PER_MINOR", "-1")
	if _, _, _, err := LoadPayments(nil, "https://control.test"); err == nil {
		t.Fatal("negative fallback pricing accepted")
	}
	t.Setenv("V3_TOPUP_CREDITS_PER_MINOR", "25")
	t.Setenv("V3_PAYMENT_CURRENCY", "usdt")
	p, prices, _, err := LoadPayments(nil, "https://control.test")
	if err != nil || len(p) != 1 || prices["stripe"].Currency != "usdt" || prices["stripe"].CreditsPerMinor != 25 {
		t.Fatalf("fallback configuration lost integer units: %+v %v", prices, err)
	}
	t.Setenv("V3_STRIPE_SECRET_KEY", "")
	t.Setenv("V3_STRIPE_WEBHOOK_SECRET", "")
	t.Setenv("V3_PAYMENT_PROVIDERS", `[{"provider":"epay","currency":"cny","credits_per_minor":20,"merchant_id":"test","secret":"test","base_url":"https://pay.test","refund_enabled":true}]`)
	_, _, refunds, err := LoadPayments(nil, "https://control.test")
	if err != nil || refunds == nil {
		t.Fatalf("explicit refund provider missing: %v", err)
	}
	if _, err := refunds.CreateRefund(context.Background(), commerce.RefundPayment{}); err == nil {
		t.Fatal("invalid refund was accepted")
	}
}

func TestSharedPaymentsServerProductQuotesAndStablecoinUnits(t *testing.T) {
	clearPaymentEnvironment(t)
	t.Setenv("V3_PAYMENT_PROVIDERS", `[{"provider":"creem","currency":"usdt","credits_per_minor":1,"api_key":"test","webhook_secret":"test","product_id":"sku","products":{"sku":{"amount_minor":1000001,"credits":10000001}}}]`)
	p, prices, _, err := LoadPayments(nil, "https://control.test")
	if err != nil {
		t.Fatal(err)
	}
	quote, err := p[0].(commerce.ProductProvider).QuoteProduct(context.Background(), "sku")
	if err != nil || quote.Currency != "usdt" || quote.AmountMinor != 1000001 || quote.Credits != 10000001 || prices["creem"].CreditsPerMinor != 1 {
		t.Fatalf("quote/pricing altered: %+v %+v %v", quote, prices, err)
	}
	for _, quote := range []string{`{"id":"wrong","amount_minor":1,"credits":1}`, `{"currency":"usd","amount_minor":1,"credits":1}`, `{"amount_minor":0,"credits":1}`, `{"amount_minor":1,"credits":-1}`} {
		t.Setenv("V3_PAYMENT_PROVIDERS", `[{"provider":"creem","currency":"usdt","credits_per_minor":1,"api_key":"sensitive-value","webhook_secret":"test","product_id":"sku","products":{"sku":`+quote+`}}]`)
		if _, _, _, err := LoadPayments(nil, "https://control.test"); err == nil || strings.Contains(err.Error(), "sensitive-value") {
			t.Fatalf("invalid product accepted or secret disclosed: %v", err)
		}
	}
}
