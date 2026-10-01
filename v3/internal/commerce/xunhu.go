package commerce

import (
	"context"
	"crypto/md5" // XunhuPay 1.1 mandates this digest for request and callback hashes.
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type XunhuConfig struct {
	AppID     string
	Secret    string
	Gateway   string
	NotifyURL string
	Client    *http.Client
	Now       func() time.Time
}

type Xunhu struct{ cfg XunhuConfig }

func NewXunhu(cfg XunhuConfig) *Xunhu {
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Xunhu{cfg: cfg}
}

func (*Xunhu) Name() string { return "xunhu" }

func (p *Xunhu) Checkout(ctx context.Context, o Order, success, _ string) (Checkout, error) {
	if p.cfg.AppID == "" || p.cfg.Secret == "" || p.cfg.Gateway == "" || p.cfg.NotifyURL == "" {
		return Checkout{}, ErrProviderUnavailable
	}
	if o.Currency != "cny" || o.AmountMinor <= 0 || o.TradeNo == "" {
		return Checkout{}, ErrInvalid
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return Checkout{}, err
	}
	form := url.Values{
		"appid": {p.cfg.AppID}, "trade_order_id": {o.TradeNo}, "title": {"CodeGo " + o.Kind},
		"total_fee": {formatMinor(o.AmountMinor)}, "notify_url": {p.cfg.NotifyURL}, "return_url": {success},
		"time": {strconv.FormatInt(p.cfg.Now().Unix(), 10)}, "version": {"1.1"}, "nonce_str": {hex.EncodeToString(nonce[:])},
	}
	form.Set("hash", xunhuSign(form, p.cfg.Secret))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.Gateway, strings.NewReader(form.Encode()))
	if err != nil {
		return Checkout{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.cfg.Client.Do(req)
	if err != nil {
		return Checkout{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Checkout{}, fmt.Errorf("xunhu: upstream status %d", resp.StatusCode)
	}
	return p.parseCheckoutReply(resp.Body, o.TradeNo)
}

// parseCheckoutReply decodes the gateway's JSON reply (which carries its own
// form-encoded-style signed fields as scalar JSON values), verifies its
// signature, and extracts the hosted payment link.
func (p *Xunhu) parseCheckoutReply(body io.Reader, tradeNo string) (Checkout, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&raw); err != nil {
		return Checkout{}, err
	}
	fields := make(url.Values, len(raw))
	for key, value := range raw {
		var scalar string
		if len(value) > 0 && value[0] == '"' {
			if err := json.Unmarshal(value, &scalar); err != nil {
				return Checkout{}, ErrProviderUnavailable
			}
		} else {
			scalar = string(value)
			if scalar == "null" || strings.HasPrefix(scalar, "{") || strings.HasPrefix(scalar, "[") {
				continue
			}
		}
		fields.Set(key, scalar)
	}
	// The payment link is accepted only from an authenticated gateway reply;
	// failure messages may contain account information and are not exposed.
	if fields.Get("errcode") != "0" || !xunhuValidSign(fields, p.cfg.Secret) ||
		(fields.Get("trade_order_id") != "" && fields.Get("trade_order_id") != tradeNo) {
		return Checkout{}, ErrProviderUnavailable
	}
	link := fields.Get("url")
	if link == "" {
		link = fields.Get("url_qrcode")
	}
	if link == "" {
		return Checkout{}, ErrProviderUnavailable
	}
	qr := fields.Get("url_qrcode")
	if !validCheckoutURL(link) || (qr != "" && !validCheckoutURL(qr)) {
		return Checkout{}, ErrProviderUnavailable
	}
	return Checkout{Reference: tradeNo, URL: link, QRCodeURL: qr}, nil
}

func (p *Xunhu) Verify(_ context.Context, _ http.Header, body []byte) (PaymentEvent, error) {
	var result PaymentEvent
	if p.cfg.AppID == "" || p.cfg.Secret == "" {
		return result, ErrProviderUnavailable
	}
	fields, err := url.ParseQuery(string(body))
	if err != nil {
		return result, ErrInvalid
	}
	for _, values := range fields {
		if len(values) != 1 {
			return result, ErrInvalid
		}
	}
	if fields.Get("appid") != p.cfg.AppID || !xunhuValidSign(fields, p.cfg.Secret) {
		return result, ErrInvalid
	}
	if fields.Get("status") != "OD" {
		return result, ErrIgnoredEvent
	}
	amount, err := parseMinor(fields.Get("total_fee"))
	trade := fields.Get("trade_order_id")
	if err != nil || amount <= 0 || trade == "" {
		return result, ErrInvalid
	}
	// Xunhu identifies callback replays by the merchant trade number. Different
	// payloads for the same number are detected by the commerce receipt hash.
	return PaymentEvent{ID: trade + ":paid", TradeNo: trade, Reference: trade,
		AmountMinor: amount, Currency: "cny", Paid: true}, nil
}

func xunhuValidSign(values url.Values, secret string) bool {
	actual := strings.ToLower(values.Get("hash"))
	expected := xunhuSign(values, secret)
	return len(actual) == len(expected) && subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func xunhuSign(values url.Values, secret string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "hash" && strings.TrimSpace(values.Get(key)) != "" {
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
