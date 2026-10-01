package commerce

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestXunhuCheckoutSignedRequestAndVerifiedResponse(t *testing.T) {
	stamp := time.Unix(1700000000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		f := r.PostForm
		if f.Get("appid") != "app-test" || f.Get("version") != "1.1" || f.Get("time") != "1700000000" || f.Get("trade_order_id") != "v3_exact" || f.Get("total_fee") != "90071992547409.93" || len(f.Get("nonce_str")) != 32 || !xunhuValidSign(f, "test-secret") {
			t.Errorf("invalid Xunhu request: %+v", f)
		}
		fields := url.Values{"errcode": {"0"}, "trade_order_id": {"v3_exact"}, "url": {"https://xunhu.test/pay"}}
		_ = json.NewEncoder(w).Encode(struct {
			Code  int    `json:"errcode"`
			Trade string `json:"trade_order_id"`
			URL   string `json:"url"`
			Hash  string `json:"hash"`
		}{0, "v3_exact", fields.Get("url"), xunhuSign(fields, "test-secret")})
	}))
	defer server.Close()
	p := NewXunhu(XunhuConfig{AppID: "app-test", Secret: "test-secret", Gateway: server.URL, NotifyURL: "https://site.test/notify", Client: server.Client(), Now: func() time.Time { return stamp }})
	got, err := p.Checkout(context.Background(), Order{TradeNo: "v3_exact", AmountMinor: 9007199254740993, Currency: "cny"}, "https://site.test/done", "")
	if err != nil || got.Reference != "v3_exact" || got.URL != "https://xunhu.test/pay" {
		t.Fatalf("checkout=%+v err=%v", got, err)
	}
}

func TestXunhuCallbackRejectsInvalidSignedClaimsAndDuplicateFields(t *testing.T) {
	p := NewXunhu(XunhuConfig{AppID: "app-test", Secret: "test-secret"})
	// Independent protocol fixture fixes the hash input and therefore detects
	// changes in field sorting, delimiter insertion or secret placement.
	canonical := "appid=app-test&status=OD&total_fee=12.50&trade_order_id=v3_paidtest-secret"
	digest := md5.Sum([]byte(canonical))
	fields := url.Values{"appid": {"app-test"}, "status": {"OD"}, "total_fee": {"12.50"}, "trade_order_id": {"v3_paid"}, "hash": {hex.EncodeToString(digest[:])}}
	e, err := p.Verify(context.Background(), nil, []byte(fields.Encode()))
	if err != nil || e.AmountMinor != 1250 || e.Reference != "v3_paid" || !e.Paid {
		t.Fatalf("event=%+v err=%v", e, err)
	}
	for _, body := range []string{strings.Replace(fields.Encode(), "12.50", "0.01", 1), fields.Encode() + "&total_fee=0.01", strings.Replace(fields.Encode(), "appid=app-test", "appid=other", 1)} {
		if _, err := p.Verify(context.Background(), nil, []byte(body)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid callback accepted: %v", err)
		}
	}
	for _, tc := range []struct {
		key, value string
		want       error
	}{
		{"status", "WP", ErrIgnoredEvent},
		{"appid", "another-merchant", ErrInvalid},
		{"total_fee", "0.00", ErrInvalid},
		{"total_fee", "1.001", ErrInvalid},
		{"total_fee", "92233720368547758.08", ErrInvalid},
		{"trade_order_id", "", ErrInvalid},
	} {
		fields.Set(tc.key, tc.value)
		fields.Set("hash", xunhuSign(fields, p.cfg.Secret))
		if _, err := p.Verify(context.Background(), nil, []byte(fields.Encode())); !errors.Is(err, tc.want) {
			t.Errorf("%s=%q err=%v want=%v", tc.key, tc.value, err, tc.want)
		}
		fields.Set("status", "OD")
		fields.Set("appid", "app-test")
		fields.Set("total_fee", "12.50")
		fields.Set("trade_order_id", "v3_paid")
	}
}

func TestXunhuCheckoutRejectsUnsignedMismatchedAndFailedReplies(t *testing.T) {
	for _, body := range []string{
		`{"errcode":0,"url":"https://xunhu.test/pay"}`,
		`{"errcode":0,"trade_order_id":"other","url":"https://xunhu.test/pay","hash":"invalid"}`,
		`{"errcode":1,"errmsg":"private-account@example.test"}`,
		`invalid-json`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		p := NewXunhu(XunhuConfig{AppID: "app-test", Secret: "test-secret", Gateway: server.URL, NotifyURL: "https://site.test/notify", Client: server.Client()})
		_, err := p.Checkout(context.Background(), Order{TradeNo: "v3_test", AmountMinor: 100, Currency: "cny"}, "", "")
		server.Close()
		if err == nil || strings.Contains(err.Error(), "private-account") {
			t.Fatalf("invalid checkout reply accepted or exposed: %v", err)
		}
	}
	for _, p := range []*Xunhu{NewXunhu(XunhuConfig{}), NewXunhu(XunhuConfig{AppID: "app-test", Secret: "test-secret"})} {
		if _, err := p.Checkout(context.Background(), Order{}, "", ""); !errors.Is(err, ErrProviderUnavailable) {
			t.Fatalf("unconfigured provider err=%v", err)
		}
	}
}
