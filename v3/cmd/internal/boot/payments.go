package boot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// V3_PAYMENT_PROVIDERS is an operator-only JSON array. Per-provider pricing is
// mandatory, so adding a CNY provider cannot inherit a USD conversion by accident.
type paymentSettings struct {
	Provider        string                           `json:"provider"`
	Currency        string                           `json:"currency"`
	CreditsPerMinor int64                            `json:"credits_per_minor"`
	APIKey          string                           `json:"api_key"`
	Secret          string                           `json:"secret"`
	WebhookSecret   string                           `json:"webhook_secret"`
	IPNSecret       string                           `json:"ipn_secret"`
	MerchantID      string                           `json:"merchant_id"`
	AppID           string                           `json:"app_id"`
	StoreID         string                           `json:"store_id"`
	ProductID       string                           `json:"product_id"`
	Products        map[string]commerce.ProductQuote `json:"products"`
	PrivateKey      string                           `json:"private_key"`
	PublicKey       string                           `json:"public_key"`
	BaseURL         string                           `json:"base_url"`
	NotifyURL       string                           `json:"notify_url"`
	PayCurrency     string                           `json:"pay_currency"`
	PaymentType     string                           `json:"payment_type"`
	PaymentTypes    []string                         `json:"payment_types"`
	PayMethodType   string                           `json:"pay_method_type"`
	PayMethodName   string                           `json:"pay_method_name"`
	Sandbox         bool                             `json:"sandbox"`
	RefundEnabled   bool                             `json:"refund_enabled"`
}

func parsePaymentSettings(raw string) ([]paymentSettings, error) {
	var settings []paymentSettings
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&settings) != nil || settings == nil || !errors.Is(d.Decode(new(any)), io.EOF) {
		return nil, errors.New("V3_PAYMENT_PROVIDERS must be a JSON array of payment settings")
	}
	seen := make(map[string]bool)
	for _, p := range settings {
		if seen[p.Provider] || p.CreditsPerMinor <= 0 || !validPaymentCurrency(p.Currency) {
			return nil, errors.New("payment providers require unique names, positive credits_per_minor and lowercase 3-12 character alphanumeric currency")
		}
		if p.RefundEnabled && p.Provider != "epay" {
			return nil, errors.New("refund_enabled requires a refund-capable epay merchant")
		}
		if p.Provider != "epay" && p.PaymentTypes != nil {
			return nil, errors.New("payment_types is supported only for epay")
		}
		seen[p.Provider] = true
		if _, err := p.build("https://configuration-check.invalid", nil); err != nil {
			return nil, err
		}
	}
	return settings, nil
}

func (p paymentSettings) build(publicURL string, buyerEmail func(context.Context, int64) (string, error)) (commerce.PaymentProvider, error) {
	callback := p.NotifyURL
	if callback == "" {
		callback = strings.TrimRight(publicURL, "/") + "/api/commerce/webhooks/" + p.Provider
	}
	missing := func(required ...string) bool {
		for _, value := range required {
			if value == "" {
				return true
			}
		}
		return false
	}
	switch p.Provider {
	case "stripe":
		if !missing(p.APIKey, p.WebhookSecret) {
			return commerce.NewStripe(commerce.StripeConfig{SecretKey: p.APIKey, WebhookSecret: p.WebhookSecret, BaseURL: p.BaseURL}), nil
		}
	case "epay":
		if !missing(p.MerchantID, p.Secret, p.BaseURL) && p.Currency == "cny" {
			types := p.PaymentTypes
			if types == nil {
				types = []string{"alipay", "wxpay"}
			}
			seen := make(map[string]bool)
			for _, method := range types {
				if method == "" || len(method) > 32 || strings.Trim(method, "abcdefghijklmnopqrstuvwxyz0123456789_-") != "" || seen[method] {
					return nil, errors.New("epay payment_types require unique nonempty cashier names")
				}
				seen[method] = true
			}
			if len(types) == 0 || (p.PaymentType != "" && !slices.Contains(types, p.PaymentType)) {
				return nil, errors.New("epay default payment_type must belong to its nonempty payment_types")
			}
			if p.NotifyURL == "" {
				callback = strings.TrimRight(publicURL, "/") + "/api/user/epay/notify"
			}
			return commerce.NewEpay(commerce.EpayConfig{MerchantID: p.MerchantID, Secret: p.Secret, BaseURL: p.BaseURL, NotifyURL: callback, PaymentType: p.PaymentType, PaymentTypes: types}), nil
		}
	case "creem":
		if !missing(p.APIKey, p.WebhookSecret, p.ProductID) {
			provider := commerce.NewCreem(commerce.CreemConfig{APIKey: p.APIKey, WebhookSecret: p.WebhookSecret, ProductID: p.ProductID, Currency: p.Currency, BaseURL: p.BaseURL, Products: p.Products})
			for id := range p.Products {
				if id == "" {
					return nil, errors.New("creem products require nonempty product IDs")
				}
				if _, err := provider.QuoteProduct(context.Background(), id); err != nil {
					return nil, errors.New("creem products require matching IDs/currency and positive integer amount_minor/credits")
				}
			}
			return provider, nil
		}
	case "xunhu":
		if !missing(p.AppID, p.Secret) && p.Currency == "cny" {
			return commerce.NewXunhu(commerce.XunhuConfig{AppID: p.AppID, Secret: p.Secret, Gateway: p.BaseURL, NotifyURL: callback}), nil
		}
	case "nowpayments":
		if !missing(p.APIKey, p.IPNSecret) {
			return commerce.NewNowPayments(commerce.NowPaymentsConfig{APIKey: p.APIKey, IPNSecret: p.IPNSecret, NotifyURL: callback, Currency: p.Currency, PayCurrency: p.PayCurrency, BaseURL: p.BaseURL}), nil
		}
	case "waffo":
		if !missing(p.APIKey, p.MerchantID, p.PrivateKey, p.PublicKey) {
			return commerce.NewWaffo(commerce.WaffoConfig{APIKey: p.APIKey, MerchantID: p.MerchantID, PrivateKey: p.PrivateKey, PublicKey: p.PublicKey, Currency: p.Currency, NotifyURL: callback, PayMethodType: p.PayMethodType, PayMethodName: p.PayMethodName, BaseURL: p.BaseURL, Sandbox: p.Sandbox, BuyerEmail: buyerEmail}), nil
		}
	case "waffo_pancake":
		if !missing(p.MerchantID, p.PrivateKey, p.PublicKey, p.StoreID, p.ProductID) {
			return commerce.NewWaffoPancake(commerce.WaffoPancakeConfig{MerchantID: p.MerchantID, PrivateKey: p.PrivateKey, WebhookPublicKey: p.PublicKey, StoreID: p.StoreID, ProductID: p.ProductID, Currency: p.Currency, BaseURL: p.BaseURL, Sandbox: p.Sandbox, BuyerEmail: buyerEmail}), nil
		}
	default:
		return nil, errors.New("unsupported payment provider")
	}
	return nil, fmt.Errorf("payment provider %s has incomplete credentials or an unsupported currency", p.Provider)
}

// LoadPayments reads the same operator configuration for control and worker.
// USDT/USDC amounts are six-decimal integer minor units, never fiat cents.
// Refunds are enabled only for an explicitly declared refund-capable Epay merchant.
func LoadPayments(pool *pgxpool.Pool, publicURL string) ([]commerce.PaymentProvider, map[string]commerce.TopupPrice, commerce.RefundProvider, error) {
	settings := []paymentSettings{}
	if raw := os.Getenv("V3_PAYMENT_PROVIDERS"); raw != "" {
		var err error
		settings, err = parsePaymentSettings(raw)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	stripeKey, stripeWebhook := os.Getenv("V3_STRIPE_SECRET_KEY"), os.Getenv("V3_STRIPE_WEBHOOK_SECRET")
	if (stripeKey == "") != (stripeWebhook == "") {
		return nil, nil, nil, errors.New("V3_STRIPE_SECRET_KEY and V3_STRIPE_WEBHOOK_SECRET must be configured together")
	}
	currency := os.Getenv("V3_PAYMENT_CURRENCY")
	if currency == "" {
		currency = "usd"
	}
	if !validPaymentCurrency(currency) {
		return nil, nil, nil, errors.New("V3_PAYMENT_CURRENCY must be a lowercase 3-12 character alphanumeric currency")
	}
	perMinor := int64(credits.PerCredit / 100)
	if raw := os.Getenv("V3_TOPUP_CREDITS_PER_MINOR"); raw != "" {
		var err error
		perMinor, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || perMinor <= 0 {
			return nil, nil, nil, errors.New("V3_TOPUP_CREDITS_PER_MINOR must be a positive integer")
		}
	}
	if stripeKey != "" {
		settings = append(settings, paymentSettings{Provider: "stripe", Currency: currency, CreditsPerMinor: perMinor, APIKey: stripeKey, WebhookSecret: stripeWebhook})
	}
	providers := make([]commerce.PaymentProvider, 0, len(settings))
	pricing := make(map[string]commerce.TopupPrice, len(settings))
	var refunds commerce.RefundProvider
	buyerEmail := func(ctx context.Context, userID int64) (string, error) {
		if pool == nil {
			return "", commerce.ErrProviderUnavailable
		}
		var email string
		err := pool.QueryRow(ctx, `SELECT COALESCE(email,'') FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL`, userID).Scan(&email)
		return email, err
	}
	for _, p := range settings {
		if _, exists := pricing[p.Provider]; exists {
			return nil, nil, nil, errors.New("stripe is configured in both environment credentials and V3_PAYMENT_PROVIDERS")
		}
		provider, err := p.build(publicURL, buyerEmail)
		if err != nil {
			return nil, nil, nil, err
		}
		providers = append(providers, provider)
		pricing[p.Provider] = commerce.TopupPrice{Currency: p.Currency, CreditsPerMinor: credits.Micro(p.CreditsPerMinor)}
		if p.RefundEnabled {
			refunds = commerce.NewEpayRefunds(commerce.EpayConfig{MerchantID: p.MerchantID, Secret: p.Secret, BaseURL: p.BaseURL}, nil)
		}
	}
	return providers, pricing, refunds, nil
}

func validPaymentCurrency(currency string) bool {
	return len(currency) >= 3 && len(currency) <= 12 && strings.Trim(currency, "abcdefghijklmnopqrstuvwxyz0123456789") == ""
}
