package commerce

import (
	"context"
	"crypto/md5" // Epay's published protocol requires MD5, not a selectable digest.
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type EpayConfig struct {
	MerchantID  string
	Secret      string
	BaseURL     string
	NotifyURL   string
	PaymentType string
}

type Epay struct{ cfg EpayConfig }

func NewEpay(cfg EpayConfig) *Epay {
	if cfg.PaymentType == "" {
		cfg.PaymentType = "alipay"
	}
	return &Epay{cfg: cfg}
}

func (*Epay) Name() string { return "epay" }

func (p *Epay) Checkout(_ context.Context, o Order, success, _ string) (Checkout, error) {
	if p.cfg.MerchantID == "" || p.cfg.Secret == "" || p.cfg.NotifyURL == "" || p.cfg.BaseURL == "" {
		return Checkout{}, ErrProviderUnavailable
	}
	if o.Currency != "cny" {
		return Checkout{}, ErrInvalid
	}
	values := url.Values{"pid": {p.cfg.MerchantID}, "type": {p.cfg.PaymentType}, "out_trade_no": {o.TradeNo},
		"notify_url": {p.cfg.NotifyURL}, "return_url": {success}, "name": {"CodeGo " + o.Kind}, "money": {formatMinor(o.AmountMinor)}}
	values.Set("sign", epaySign(values, p.cfg.Secret))
	values.Set("sign_type", "MD5")
	return Checkout{Reference: o.TradeNo, URL: strings.TrimRight(p.cfg.BaseURL, "/") + "/submit.php?" + values.Encode()}, nil
}

func (p *Epay) Verify(_ context.Context, _ http.Header, body []byte) (PaymentEvent, error) {
	var e PaymentEvent
	if p.cfg.Secret == "" || p.cfg.MerchantID == "" {
		return e, ErrProviderUnavailable
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return e, ErrInvalid
	}
	for _, items := range values {
		if len(items) != 1 {
			return e, ErrInvalid
		}
	}
	if values.Get("sign_type") != "MD5" || values.Get("pid") != p.cfg.MerchantID {
		return e, ErrInvalid
	}
	expected := epaySign(values, p.cfg.Secret)
	actual := strings.ToLower(values.Get("sign"))
	if len(actual) != len(expected) || subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		return e, ErrInvalid
	}
	if values.Get("trade_status") != "TRADE_SUCCESS" && values.Get("trade_status") != "TRADE_FINISHED" {
		return e, ErrIgnoredEvent
	}
	amount, err := parseMinor(values.Get("money"))
	if err != nil || amount <= 0 || values.Get("trade_no") == "" || values.Get("out_trade_no") == "" {
		return e, ErrInvalid
	}
	e = PaymentEvent{ID: values.Get("trade_no"), TradeNo: values.Get("out_trade_no"), Reference: values.Get("out_trade_no"), AmountMinor: amount, Currency: "cny", Paid: true}
	return e, nil
}

func epaySign(values url.Values, secret string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "sign" && key != "sign_type" && values.Get(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var canonical strings.Builder
	for i, key := range keys {
		if i > 0 {
			canonical.WriteByte('&')
		}
		canonical.WriteString(key)
		canonical.WriteByte('=')
		canonical.WriteString(values.Get(key))
	}
	canonical.WriteString(secret)
	digest := md5.Sum([]byte(canonical.String()))
	return hex.EncodeToString(digest[:])
}

func formatMinor(amount int64) string { return fmt.Sprintf("%d.%02d", amount/100, amount%100) }

func parseMinor(raw string) (int64, error) {
	whole, fraction, hasDot := strings.Cut(raw, ".")
	if !hasDot {
		fraction = "00"
	}
	if len(fraction) == 1 {
		fraction += "0"
	}
	if whole == "" || len(fraction) != 2 {
		return 0, ErrInvalid
	}
	for _, ch := range whole + fraction {
		if ch < '0' || ch > '9' {
			return 0, ErrInvalid
		}
	}
	return strconv.ParseInt(whole+fraction, 10, 64)
}
