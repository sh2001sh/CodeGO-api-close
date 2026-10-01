package commerce

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

type NowPaymentsConfig struct {
	APIKey      string
	IPNSecret   string
	NotifyURL   string
	Currency    string
	PayCurrency string
	BaseURL     string
	Client      *http.Client
}

type NowPayments struct{ cfg NowPaymentsConfig }

func NewNowPayments(cfg NowPaymentsConfig) *NowPayments {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.nowpayments.io/v1"
	}
	if cfg.Currency == "" {
		cfg.Currency = "usd"
	}
	if cfg.PayCurrency == "" {
		cfg.PayCurrency = "usdttrc20"
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 15 * time.Second}
	}
	return &NowPayments{cfg: cfg}
}

func (*NowPayments) Name() string { return "nowpayments" }

func (p *NowPayments) Checkout(ctx context.Context, o Order, success, cancel string) (Checkout, error) {
	if p.cfg.APIKey == "" || p.cfg.NotifyURL == "" {
		return Checkout{}, ErrProviderUnavailable
	}
	if o.Currency != p.cfg.Currency || o.AmountMinor <= 0 || o.TradeNo == "" {
		return Checkout{}, ErrInvalid
	}
	// A hosted invoice always has a payment URL. Direct /payment may return
	// only a blockchain address, which cannot fulfill the checkout contract.
	body, err := json.Marshal(struct {
		PriceAmount      json.Number `json:"price_amount"`
		PriceCurrency    string      `json:"price_currency"`
		PayCurrency      string      `json:"pay_currency"`
		OrderID          string      `json:"order_id"`
		OrderDescription string      `json:"order_description"`
		NotifyURL        string      `json:"ipn_callback_url"`
		SuccessURL       string      `json:"success_url"`
		CancelURL        string      `json:"cancel_url"`
	}{json.Number(formatCurrencyMinor(o.AmountMinor, o.Currency)), o.Currency, p.cfg.PayCurrency, o.TradeNo,
		"CodeGo " + o.Kind, p.cfg.NotifyURL, success, cancel})
	if err != nil {
		return Checkout{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.cfg.BaseURL, "/")+"/invoice", bytes.NewReader(body))
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
		return Checkout{}, fmt.Errorf("nowpayments: upstream status %d", resp.StatusCode)
	}
	var reply struct {
		ID  json.RawMessage `json:"id"`
		URL string          `json:"invoice_url"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&reply); err != nil {
		return Checkout{}, err
	}
	id, err := nowPaymentsID(reply.ID)
	if err != nil || reply.URL == "" {
		return Checkout{}, ErrProviderUnavailable
	}
	return Checkout{Reference: id, URL: reply.URL}, nil
}

func (p *NowPayments) Verify(_ context.Context, header http.Header, body []byte) (PaymentEvent, error) {
	var result PaymentEvent
	if p.cfg.IPNSecret == "" {
		return result, ErrProviderUnavailable
	}
	if err := verifyNowPaymentsSignature(body, header.Get("x-nowpayments-sig"), p.cfg.IPNSecret); err != nil {
		return result, err
	}
	var event struct {
		PaymentID    json.RawMessage `json:"payment_id"`
		InvoiceID    json.RawMessage `json:"invoice_id"`
		OrderID      string          `json:"order_id"`
		Status       string          `json:"payment_status"`
		PriceAmount  json.RawMessage `json:"price_amount"`
		Currency     string          `json:"price_currency"`
		PayAmount    json.RawMessage `json:"pay_amount"`
		ActuallyPaid json.RawMessage `json:"actually_paid"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return result, ErrInvalid
	}
	if event.Status != "finished" {
		return result, ErrIgnoredEvent
	}
	id, idErr := nowPaymentsID(event.PaymentID)
	invoice, invoiceErr := nowPaymentsID(event.InvoiceID)
	price, priceErr := nowPaymentsDecimal(event.PriceAmount)
	paid, paidErr := nowPaymentsDecimal(event.ActuallyPaid)
	expected, expectedErr := nowPaymentsDecimal(event.PayAmount)
	if idErr != nil || invoiceErr != nil || priceErr != nil || paidErr != nil || expectedErr != nil || event.OrderID == "" {
		return result, ErrInvalid
	}
	// Never treat a signed underpayment as the full order amount. Both paid
	// quantities are in the same blockchain currency, with exact rational math.
	if paid.Cmp(expected) < 0 {
		return result, ErrPaymentMismatch
	}
	minor := new(big.Rat).Mul(price, big.NewRat(paymentScale(p.cfg.Currency), 1))
	if !minor.IsInt() || !minor.Num().IsInt64() || minor.Sign() <= 0 || strings.ToLower(event.Currency) != p.cfg.Currency {
		return result, ErrInvalid
	}
	return PaymentEvent{ID: id + ":finished", TradeNo: event.OrderID, Reference: invoice,
		AmountMinor: minor.Num().Int64(), Currency: p.cfg.Currency, Paid: true}, nil
}

// verifyNowPaymentsSignature re-encodes the body as NOWPayments' recursively
// sorted canonical JSON and checks the HMAC-SHA512 signature header against
// it, since NOWPayments signs that canonical form rather than the raw body.
func verifyNowPaymentsSignature(body []byte, signatureHeader, secret string) error {
	// UseNumber preserves large payment identifiers and exact decimal amounts.
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var canonical map[string]any
	if err := decoder.Decode(&canonical); err != nil || canonical == nil {
		return ErrInvalid
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return ErrInvalid
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(canonical); err != nil {
		return ErrInvalid
	}
	actual, err := hex.DecodeString(strings.TrimSpace(signatureHeader))
	if err != nil {
		return ErrInvalid
	}
	mac := hmac.New(sha512.New, []byte(secret))
	_, _ = mac.Write(bytes.TrimSuffix(encoded.Bytes(), []byte{'\n'}))
	if !hmac.Equal(actual, mac.Sum(nil)) {
		return ErrInvalid
	}
	return nil
}

func nowPaymentsID(raw json.RawMessage) (string, error) {
	value, err := nowPaymentsValue(raw)
	if err != nil || len(value) == 0 || len(value) > 64 {
		return "", ErrInvalid
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return "", ErrInvalid
		}
	}
	if strings.Trim(value, "0") == "" {
		return "", ErrInvalid
	}
	return value, nil
}

func nowPaymentsValue(raw json.RawMessage) (string, error) {
	var value string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", ErrInvalid
		}
	} else {
		value = string(raw)
	}
	return value, nil
}

func nowPaymentsDecimal(raw json.RawMessage) (*big.Rat, error) {
	value, err := nowPaymentsValue(raw)
	if err != nil || value == "" || len(value) > 80 {
		return nil, ErrInvalid
	}
	dots := 0
	for _, ch := range value {
		if ch == '.' {
			dots++
		} else if ch < '0' || ch > '9' {
			return nil, ErrInvalid
		}
	}
	if dots > 1 || value[0] == '.' || value[len(value)-1] == '.' {
		return nil, ErrInvalid
	}
	amount, ok := new(big.Rat).SetString(value)
	if !ok || amount.Sign() <= 0 {
		return nil, ErrInvalid
	}
	return amount, nil
}
