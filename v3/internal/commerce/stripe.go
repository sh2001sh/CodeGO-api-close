package commerce

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type StripeConfig struct {
	SecretKey          string
	WebhookSecret      string
	BaseURL            string
	Client             *http.Client
	Now                func() time.Time
	SignatureTolerance time.Duration
}

type Stripe struct{ cfg StripeConfig }

func NewStripe(cfg StripeConfig) *Stripe {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.stripe.com"
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.SignatureTolerance == 0 {
		cfg.SignatureTolerance = 5 * time.Minute
	}
	return &Stripe{cfg: cfg}
}

func (s *Stripe) Name() string { return "stripe" }

func (s *Stripe) Checkout(ctx context.Context, o Order, success, cancel string) (Checkout, error) {
	var result Checkout
	if s.cfg.SecretKey == "" {
		return result, ErrProviderUnavailable
	}
	form := url.Values{
		"mode": {"payment"}, "success_url": {success}, "cancel_url": {cancel},
		"client_reference_id": {o.TradeNo}, "metadata[trade_no]": {o.TradeNo},
		"payment_intent_data[metadata][trade_no]": {o.TradeNo},
		"line_items[0][quantity]":                 {"1"}, "line_items[0][price_data][currency]": {o.Currency},
		"line_items[0][price_data][unit_amount]":        {strconv.FormatInt(o.AmountMinor, 10)},
		"line_items[0][price_data][product_data][name]": {"CodeGo " + o.Kind},
	}
	var response struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := s.request(ctx, http.MethodPost, "/v1/checkout/sessions", o.TradeNo, form, &response); err != nil {
		return result, err
	}
	if response.ID == "" || response.URL == "" {
		return result, ErrProviderUnavailable
	}
	return Checkout{Reference: response.ID, URL: response.URL}, nil
}

func (s *Stripe) request(ctx context.Context, method, path, key string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.cfg.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.cfg.SecretKey, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Idempotency-Key", key)
	resp, err := s.cfg.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Provider bodies can contain account identifiers. Do not surface them.
		return fmt.Errorf("stripe: upstream status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func (s *Stripe) Verify(_ context.Context, header http.Header, body []byte) (PaymentEvent, error) {
	var event PaymentEvent
	if err := s.verifySignature(header.Get("Stripe-Signature"), body); err != nil {
		return event, err
	}
	var wire struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID                string `json:"id"`
				ClientReferenceID string `json:"client_reference_id"`
				AmountTotal       int64  `json:"amount_total"`
				Amount            int64  `json:"amount"`
				AmountRefunded    int64  `json:"amount_refunded"`
				Currency          string `json:"currency"`
				PaymentStatus     string `json:"payment_status"`
				Metadata          struct {
					TradeNo string `json:"trade_no"`
				} `json:"metadata"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return event, ErrInvalid
	}
	o := wire.Data.Object
	event = PaymentEvent{ID: wire.ID, TradeNo: o.ClientReferenceID, Reference: o.ID, AmountMinor: o.AmountTotal, Currency: o.Currency}
	if event.TradeNo == "" {
		event.TradeNo = o.Metadata.TradeNo
	}
	switch wire.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		if o.PaymentStatus != "paid" {
			return event, ErrIgnoredEvent
		}
		event.Paid = true
	case "checkout.session.async_payment_failed":
		event.State = "failed"
	case "checkout.session.expired":
		event.State = "expired"
	case "charge.refunded":
		// amount_refunded is a cumulative total. The ledger advances toward
		// this total, so reordered or repeated partial callbacks cannot over-debit.
		if o.Amount <= 0 || o.AmountRefunded <= 0 || o.AmountRefunded > o.Amount {
			return event, ErrPaymentMismatch
		}
		event.AmountMinor, event.Refunded = o.AmountRefunded, true
	default:
		return event, ErrIgnoredEvent
	}
	if event.ID == "" || event.TradeNo == "" || event.AmountMinor <= 0 || !validCurrency(event.Currency) {
		return event, ErrInvalid
	}
	return event, nil
}

func (s *Stripe) verifySignature(header string, body []byte) error {
	if s.cfg.WebhookSecret == "" {
		return ErrProviderUnavailable
	}
	var timestamp string
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			if timestamp != "" {
				return ErrInvalid
			}
			timestamp = value
		case "v1":
			signatures = append(signatures, value)
		}
	}
	sec, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || len(signatures) == 0 {
		return ErrInvalid
	}
	stamp := time.Unix(sec, 0)
	now := s.cfg.Now()
	if stamp.Before(now.Add(-s.cfg.SignatureTolerance)) || stamp.After(now.Add(s.cfg.SignatureTolerance)) {
		return ErrInvalid
	}
	mac := hmac.New(sha256.New, []byte(s.cfg.WebhookSecret))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)
	for _, candidate := range signatures {
		decoded, err := hex.DecodeString(candidate)
		if err == nil && hmac.Equal(expected, decoded) {
			return nil
		}
	}
	return fmt.Errorf("%w: payment signature", ErrInvalid)
}
