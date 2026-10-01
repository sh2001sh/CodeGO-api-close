package commerce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type WaffoConfig struct {
	APIKey        string
	PrivateKey    string
	PublicKey     string
	MerchantID    string
	Currency      string
	NotifyURL     string
	PayMethodType string
	PayMethodName string
	Sandbox       bool
	BaseURL       string
	Client        *http.Client
	Now           func() time.Time
	BuyerEmail    func(context.Context, int64) (string, error)
}

type Waffo struct{ cfg WaffoConfig }

func NewWaffo(cfg WaffoConfig) *Waffo {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.waffo.com/api/v1"
		if cfg.Sandbox {
			cfg.BaseURL = "https://api-sandbox.waffo.com/api/v1"
		}
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
	return &Waffo{cfg: cfg}
}

func (*Waffo) Name() string { return "waffo" }

func (p *Waffo) Checkout(ctx context.Context, o Order, success, cancel string) (Checkout, error) {
	if p.cfg.APIKey == "" || p.cfg.MerchantID == "" || p.cfg.PrivateKey == "" || p.cfg.PublicKey == "" || p.cfg.NotifyURL == "" {
		return Checkout{}, ErrProviderUnavailable
	}
	if o.AmountMinor <= 0 || o.TradeNo == "" || o.Currency != p.cfg.Currency || !validCurrency(o.Currency) {
		return Checkout{}, ErrInvalid
	}
	req, err := p.buildCheckoutRequest(ctx, o, success, cancel)
	if err != nil {
		return Checkout{}, err
	}
	resp, err := p.cfg.Client.Do(req)
	if err != nil {
		return Checkout{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Checkout{}, fmt.Errorf("waffo: upstream status %d", resp.StatusCode)
	}
	replyBody, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return Checkout{}, err
	}
	if len(replyBody) > 1<<20 {
		return Checkout{}, ErrProviderUnavailable
	}
	if sig := resp.Header.Get("X-SIGNATURE"); sig != "" {
		if err := waffoVerify(replyBody, sig, p.cfg.PublicKey); err != nil {
			return Checkout{}, err
		}
	}
	return p.parseCheckoutReply(replyBody, o.TradeNo)
}

// buildCheckoutRequest assembles and signs the order-create payload.
func (p *Waffo) buildCheckoutRequest(ctx context.Context, o Order, success, cancel string) (*http.Request, error) {
	email := ""
	if p.cfg.BuyerEmail != nil {
		var err error
		email, err = p.cfg.BuyerEmail(ctx, o.UserID)
		if err != nil {
			return nil, err
		}
	}
	payload := map[string]any{
		"paymentRequestId": o.TradeNo, "merchantOrderId": o.TradeNo,
		"orderAmount": formatCurrencyMinor(o.AmountMinor, o.Currency), "orderCurrency": strings.ToUpper(o.Currency),
		"orderDescription": "CodeGo " + o.Kind, "orderRequestedAt": p.cfg.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		"notifyUrl": p.cfg.NotifyURL, "successRedirectUrl": success, "failedRedirectUrl": cancel, "cancelRedirectUrl": cancel,
		"merchantInfo": map[string]string{"merchantId": p.cfg.MerchantID},
		"userInfo":     map[string]string{"userId": strconv.FormatInt(o.UserID, 10), "userEmail": email, "userTerminal": "WEB"},
		"paymentInfo":  map[string]string{"productName": "ONE_TIME_PAYMENT", "payMethodType": p.cfg.PayMethodType, "payMethodName": p.cfg.PayMethodName},
	}
	if !o.ExpiresAt.IsZero() {
		payload["orderExpiredAt"] = o.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	signature, err := waffoSign(body, p.cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.cfg.BaseURL, "/")+"/order/create", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", p.cfg.APIKey)
	req.Header.Set("X-SIGNATURE", signature)
	req.Header.Set("X-API-VERSION", "1.0.0")
	req.Header.Set("Idempotency-Key", o.TradeNo)
	return req, nil
}

// parseCheckoutReply validates the order-create response and extracts the
// hosted payment URL, which may arrive either as a plain string or as a
// JSON-encoded action object with a webUrl field.
func (p *Waffo) parseCheckoutReply(replyBody []byte, tradeNo string) (Checkout, error) {
	var reply struct {
		Code string `json:"code"`
		Data struct {
			PaymentRequestID string `json:"paymentRequestId"`
			MerchantOrderID  string `json:"merchantOrderId"`
			OrderAction      string `json:"orderAction"`
		} `json:"data"`
	}
	if err := json.Unmarshal(replyBody, &reply); err != nil {
		return Checkout{}, ErrProviderUnavailable
	}
	if reply.Code != "0" {
		return Checkout{}, ErrProviderUnavailable
	}
	if (reply.Data.PaymentRequestID != "" && reply.Data.PaymentRequestID != tradeNo) || (reply.Data.MerchantOrderID != "" && reply.Data.MerchantOrderID != tradeNo) {
		return Checkout{}, ErrPaymentMismatch
	}
	payURL := reply.Data.OrderAction
	var action struct {
		WebURL string `json:"webUrl"`
	}
	if json.Unmarshal([]byte(payURL), &action) == nil {
		payURL = action.WebURL
	}
	if !waffoPaymentURL(payURL) {
		return Checkout{}, ErrProviderUnavailable
	}
	return Checkout{Reference: tradeNo, URL: payURL}, nil
}

func (p *Waffo) Verify(_ context.Context, header http.Header, body []byte) (PaymentEvent, error) {
	var result PaymentEvent
	if err := waffoVerify(body, header.Get("X-SIGNATURE"), p.cfg.PublicKey); err != nil {
		return result, err
	}
	var event struct {
		EventType string `json:"eventType"`
		Result    struct {
			PaymentRequestID string `json:"paymentRequestId"`
			MerchantOrderID  string `json:"merchantOrderId"`
			OrderStatus      string `json:"orderStatus"`
			OrderCurrency    string `json:"orderCurrency"`
			OrderAmount      string `json:"orderAmount"`
			MerchantInfo     struct {
				MerchantID string `json:"merchantId"`
			} `json:"merchantInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return result, ErrInvalid
	}
	if event.EventType != "PAYMENT_NOTIFICATION" || event.Result.OrderStatus != "PAY_SUCCESS" {
		return result, ErrIgnoredEvent
	}
	wire := event.Result
	if wire.MerchantInfo.MerchantID != "" && wire.MerchantInfo.MerchantID != p.cfg.MerchantID {
		return result, ErrPaymentMismatch
	}
	currency := strings.ToLower(wire.OrderCurrency)
	amount, err := parseCurrencyMinor(wire.OrderAmount, currency)
	if err != nil || amount <= 0 || wire.MerchantOrderID == "" || !validCurrency(currency) || currency != p.cfg.Currency {
		return result, ErrInvalid
	}
	ref := wire.PaymentRequestID
	if ref == "" {
		ref = wire.MerchantOrderID
	}
	return PaymentEvent{ID: "payment:" + ref, TradeNo: wire.MerchantOrderID, Reference: ref, AmountMinor: amount, Currency: currency, Paid: true}, nil
}

// WebhookResponse implements the signed acknowledgment required by Waffo.
func (p *Waffo) WebhookResponse(success bool) (http.Header, []byte, error) {
	message := "failed"
	if success {
		message = "success"
	}
	body := []byte(`{"message":"` + message + `"}`)
	signature, err := waffoSign(body, p.cfg.PrivateKey)
	if err != nil {
		return nil, nil, err
	}
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("X-SIGNATURE", signature)
	return header, body, nil
}
