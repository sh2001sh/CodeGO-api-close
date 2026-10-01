package commerce

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type CreemConfig struct {
	APIKey        string
	WebhookSecret string
	ProductID     string
	Currency      string
	BaseURL       string
	Client        *http.Client
	Products      map[string]ProductQuote
}

type Creem struct{ cfg CreemConfig }

func NewCreem(cfg CreemConfig) *Creem {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.creem.io"
	}
	if cfg.Currency == "" {
		cfg.Currency = "usd"
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Creem{cfg: cfg}
}

func (*Creem) Name() string { return "creem" }

func (p *Creem) Checkout(ctx context.Context, o Order, success, _ string) (Checkout, error) {
	productID := p.cfg.ProductID
	if o.ProductID != "" {
		if _, ok := p.cfg.Products[o.ProductID]; !ok {
			return Checkout{}, ErrInvalid
		}
		productID = o.ProductID
	}
	if p.cfg.APIKey == "" || productID == "" {
		return Checkout{}, ErrProviderUnavailable
	}
	if o.Currency != p.cfg.Currency {
		return Checkout{}, ErrInvalid
	}
	payload, err := json.Marshal(struct {
		ProductID   string            `json:"product_id"`
		RequestID   string            `json:"request_id"`
		Units       int               `json:"units"`
		CustomPrice int64             `json:"custom_price"`
		SuccessURL  string            `json:"success_url"`
		Metadata    map[string]string `json:"metadata"`
	}{productID, o.TradeNo, 1, o.AmountMinor, success, map[string]string{"trade_no": o.TradeNo}})
	if err != nil {
		return Checkout{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.cfg.BaseURL, "/")+"/v1/checkouts", bytes.NewReader(payload))
	if err != nil {
		return Checkout{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.cfg.APIKey)
	req.Header.Set("Idempotency-Key", o.TradeNo)
	resp, err := p.cfg.Client.Do(req)
	if err != nil {
		return Checkout{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Checkout{}, fmt.Errorf("creem: upstream status %d", resp.StatusCode)
	}
	var reply struct {
		ID  string `json:"id"`
		URL string `json:"checkout_url"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&reply); err != nil {
		return Checkout{}, err
	}
	if reply.ID == "" || reply.URL == "" {
		return Checkout{}, ErrProviderUnavailable
	}
	return Checkout{Reference: reply.ID, URL: reply.URL}, nil
}

func (p *Creem) QuoteProduct(_ context.Context, id string) (ProductQuote, error) {
	quote, ok := p.cfg.Products[id]
	if !ok {
		return ProductQuote{}, ErrNotFound
	}
	if quote.ID == "" {
		quote.ID = id
	}
	if quote.Currency == "" {
		quote.Currency = p.cfg.Currency
	}
	if quote.ID != id || quote.AmountMinor <= 0 || quote.Credits <= 0 || quote.Currency != p.cfg.Currency {
		return ProductQuote{}, ErrInvalid
	}
	return quote, nil
}

func (p *Creem) Verify(_ context.Context, header http.Header, body []byte) (PaymentEvent, error) {
	var result PaymentEvent
	if p.cfg.WebhookSecret == "" {
		return result, ErrProviderUnavailable
	}
	actual, err := hex.DecodeString(header.Get("creem-signature"))
	if err != nil {
		return result, ErrInvalid
	}
	mac := hmac.New(sha256.New, []byte(p.cfg.WebhookSecret))
	_, _ = mac.Write(body)
	if !hmac.Equal(actual, mac.Sum(nil)) {
		return result, ErrInvalid
	}
	var event struct {
		ID     string `json:"id"`
		Type   string `json:"eventType"`
		Object struct {
			ID        string `json:"id"`
			RequestID string `json:"request_id"`
			Order     struct {
				Status   string `json:"status"`
				Amount   int64  `json:"amount"`
				Currency string `json:"currency"`
			} `json:"order"`
		} `json:"object"`
	}
	if err = json.Unmarshal(body, &event); err != nil {
		return result, ErrInvalid
	}
	if event.Type != "checkout.completed" || event.Object.Order.Status != "paid" {
		return result, ErrIgnoredEvent
	}
	result = PaymentEvent{ID: event.ID, TradeNo: event.Object.RequestID, Reference: event.Object.ID,
		AmountMinor: event.Object.Order.Amount, Currency: strings.ToLower(event.Object.Order.Currency), Paid: true}
	if result.ID == "" || result.TradeNo == "" || result.Reference == "" || result.AmountMinor <= 0 || !validCurrency(result.Currency) {
		return result, ErrInvalid
	}
	return result, nil
}
