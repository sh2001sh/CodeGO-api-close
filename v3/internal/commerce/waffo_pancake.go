package commerce

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const waffoPancakeCheckoutPath = "/v1/actions/checkout/create-session"

type WaffoPancakeConfig struct {
	MerchantID         string
	PrivateKey         string
	WebhookPublicKey   string
	StoreID            string
	ProductID          string
	Currency           string
	Sandbox            bool
	BaseURL            string
	Client             *http.Client
	Now                func() time.Time
	SignatureTolerance time.Duration
	BuyerEmail         func(context.Context, int64) (string, error)
}

type WaffoPancake struct{ cfg WaffoPancakeConfig }

func NewWaffoPancake(cfg WaffoPancakeConfig) *WaffoPancake {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://waffo-pancake-auth-service.vercel.app"
	}
	if cfg.Currency == "" {
		cfg.Currency = "usd"
	}
	cfg.Currency = strings.ToLower(cfg.Currency)
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.SignatureTolerance <= 0 {
		cfg.SignatureTolerance = 5 * time.Minute
	}
	return &WaffoPancake{cfg: cfg}
}

func (*WaffoPancake) Name() string { return "waffo_pancake" }

func (p *WaffoPancake) mode() string {
	if p.cfg.Sandbox {
		return "test"
	}
	return "prod"
}

func (p *WaffoPancake) Checkout(ctx context.Context, o Order, success, _ string) (Checkout, error) {
	if p.cfg.MerchantID == "" || p.cfg.PrivateKey == "" || p.cfg.WebhookPublicKey == "" || p.cfg.StoreID == "" || p.cfg.ProductID == "" {
		return Checkout{}, ErrProviderUnavailable
	}
	if o.AmountMinor <= 0 || o.TradeNo == "" || o.Currency != p.cfg.Currency || !validCurrency(o.Currency) {
		return Checkout{}, ErrInvalid
	}
	req, err := p.buildCheckoutRequest(ctx, o, success)
	if err != nil {
		return Checkout{}, err
	}
	resp, err := p.cfg.Client.Do(req)
	if err != nil {
		return Checkout{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Checkout{}, fmt.Errorf("waffo pancake: upstream status %d", resp.StatusCode)
	}
	var reply struct {
		Data struct {
			SessionID   string `json:"sessionId"`
			CheckoutURL string `json:"checkoutUrl"`
			OrderID     string `json:"orderId"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return Checkout{}, err
	}
	if len(responseBody) > 1<<20 || json.Unmarshal(responseBody, &reply) != nil || len(reply.Errors) != 0 || reply.Data.SessionID == "" || reply.Data.OrderID == "" || !waffoPaymentURL(reply.Data.CheckoutURL) {
		return Checkout{}, ErrProviderUnavailable
	}
	// The provider generates its order ID. Fulfillment resolves the local
	// trade number from this stored reference, never from buyer email.
	return Checkout{Reference: reply.Data.OrderID, URL: reply.Data.CheckoutURL}, nil
}

// buildCheckoutRequest assembles and signs the checkout-session payload.
func (p *WaffoPancake) buildCheckoutRequest(ctx context.Context, o Order, success string) (*http.Request, error) {
	email := ""
	if p.cfg.BuyerEmail != nil {
		var err error
		email, err = p.cfg.BuyerEmail(ctx, o.UserID)
		if err != nil {
			return nil, err
		}
	}
	seconds := int64(30 * 60)
	if !o.ExpiresAt.IsZero() {
		seconds = int64(o.ExpiresAt.Sub(p.cfg.Now()) / time.Second)
		if seconds <= 0 {
			return nil, ErrInvalid
		}
	}
	payload := map[string]any{
		"storeId": p.cfg.StoreID, "productId": p.cfg.ProductID, "productType": "onetime",
		"currency": strings.ToUpper(o.Currency), "successUrl": success, "expiresInSeconds": seconds,
		// Freeze the total the customer is charged; callbacks must match it.
		"priceSnapshot": map[string]any{"amount": formatCurrencyMinor(o.AmountMinor, o.Currency), "taxIncluded": true, "taxCategory": "saas"},
	}
	if email != "" {
		payload["buyerEmail"] = email
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	timestamp := strconv.FormatInt(p.cfg.Now().Unix(), 10)
	digest := sha256.Sum256(body)
	canonical := http.MethodPost + "\n" + waffoPancakeCheckoutPath + "\n" + timestamp + "\n" + base64.StdEncoding.EncodeToString(digest[:])
	signature, err := waffoSign([]byte(canonical), p.cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.cfg.BaseURL, "/")+waffoPancakeCheckoutPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Merchant-Id", p.cfg.MerchantID)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", signature)
	req.Header.Set("X-Environment", p.mode())
	req.Header.Set("Idempotency-Key", o.TradeNo)
	return req, nil
}

func (p *WaffoPancake) Verify(_ context.Context, header http.Header, body []byte) (PaymentEvent, error) {
	var result PaymentEvent
	timestamp, signature, err := parseWaffoPancakeSignatureHeader(header.Get("X-Waffo-Signature"))
	if err != nil {
		return result, err
	}
	ms, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || timestamp == "" || signature == "" {
		return result, ErrInvalid
	}
	stamp, now := time.UnixMilli(ms), p.cfg.Now()
	if stamp.Before(now.Add(-p.cfg.SignatureTolerance)) || stamp.After(now.Add(p.cfg.SignatureTolerance)) {
		return result, ErrInvalid
	}
	if err := waffoVerify(append([]byte(timestamp+"."), body...), signature, p.cfg.WebhookPublicKey); err != nil {
		return result, err
	}
	var event struct {
		ID        string `json:"id"`
		EventID   string `json:"eventId"`
		EventType string `json:"eventType"`
		StoreID   string `json:"storeId"`
		Mode      string `json:"mode"`
		Data      struct {
			OrderID  string          `json:"orderId"`
			Currency string          `json:"currency"`
			Amount   json.RawMessage `json:"amount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return result, ErrInvalid
	}
	if event.Mode != p.mode() || event.StoreID != p.cfg.StoreID {
		return result, ErrPaymentMismatch
	}
	if event.EventType != "order.completed" {
		return result, ErrIgnoredEvent
	}
	amountString := string(event.Data.Amount)
	if strings.HasPrefix(amountString, `"`) {
		if err := json.Unmarshal(event.Data.Amount, &amountString); err != nil {
			return result, ErrInvalid
		}
	}
	currency := strings.ToLower(event.Data.Currency)
	amount, err := parseCurrencyMinor(amountString, currency)
	if event.ID == "" {
		event.ID = event.EventID
	}
	if err != nil || amount <= 0 || event.ID == "" || event.Data.OrderID == "" || !validCurrency(currency) || currency != p.cfg.Currency {
		return result, ErrInvalid
	}
	return PaymentEvent{ID: event.ID, Reference: event.Data.OrderID, AmountMinor: amount, Currency: currency, Paid: true}, nil
}

// parseWaffoPancakeSignatureHeader splits the "t=...,v1=..." signature header
// into its timestamp and signature components, rejecting duplicates.
func parseWaffoPancakeSignatureHeader(header string) (timestamp, signature string, err error) {
	for _, part := range strings.Split(header, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			return "", "", ErrInvalid
		}
		switch key {
		case "t":
			if timestamp != "" {
				return "", "", ErrInvalid
			}
			timestamp = value
		case "v1":
			if signature != "" {
				return "", "", ErrInvalid
			}
			signature = value
		}
	}
	return timestamp, signature, nil
}
