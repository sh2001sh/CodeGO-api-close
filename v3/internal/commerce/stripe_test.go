package commerce

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func stripeSignature(secret string, now time.Time, body string) string {
	stamp := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(stamp + "." + body))
	return "t=" + stamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestStripeVerifiedAmountAndSignatureReplayWindow(t *testing.T) {
	now := time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC)
	s := NewStripe(StripeConfig{WebhookSecret: "test-signing-secret", Now: func() time.Time { return now }})
	body := `{"id":"evt_1","type":"checkout.session.completed","data":{"object":{"id":"cs_1","client_reference_id":"v3_1","amount_total":499,"currency":"usd","payment_status":"paid"}}}`
	head := http.Header{"Stripe-Signature": {stripeSignature(s.cfg.WebhookSecret, now, body)}}
	e, err := s.Verify(context.Background(), head, []byte(body))
	if err != nil || !e.Paid || e.AmountMinor != 499 || e.TradeNo != "v3_1" || e.Reference != "cs_1" {
		t.Fatalf("event=%+v err=%v", e, err)
	}
	for _, invalid := range []struct {
		name  string
		stamp time.Time
		body  string
	}{
		{"old callback", now.Add(-6 * time.Minute), body},
		{"future callback", now.Add(6 * time.Minute), body},
		{"tampered amount", now, strings.Replace(body, "499", "1", 1)},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			candidate := http.Header{"Stripe-Signature": {stripeSignature(s.cfg.WebhookSecret, invalid.stamp, body)}}
			if _, err := s.Verify(context.Background(), candidate, []byte(invalid.body)); err == nil {
				t.Fatal("invalid signature accepted")
			}
		})
	}
	rotated := head.Clone()
	rotated.Set("Stripe-Signature", head.Get("Stripe-Signature")+",v1=bad-old-signature")
	if _, err := s.Verify(context.Background(), rotated, []byte(body)); err != nil {
		t.Fatalf("signature rotation: %v", err)
	}
}

func TestStripeCheckoutUsesOrderPriceAndIdempotency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/checkout/sessions" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("line_items[0][price_data][unit_amount]") != "1200" || r.Form.Get("client_reference_id") != "order-1" ||
			r.Header.Get("Idempotency-Key") != "order-1" || r.Form.Get("payment_intent_data[metadata][trade_no]") != "order-1" {
			t.Errorf("missing server pricing or idempotency: %v", r.Form)
		}
		_, _ = w.Write([]byte(`{"id":"cs_1","url":"https://checkout.stripe.com/c/pay/cs_1"}`))
	}))
	defer server.Close()
	s := NewStripe(StripeConfig{SecretKey: "test-key", BaseURL: server.URL, Client: server.Client()})
	checkout, err := s.Checkout(context.Background(), Order{TradeNo: "order-1", Kind: "topup", Currency: "usd", AmountMinor: 1200}, "https://site/success", "https://site/cancel")
	if err != nil || checkout.Reference != "cs_1" {
		t.Fatalf("checkout=%+v err=%v", checkout, err)
	}
}

func TestStripeRefundCarriesCumulativeAmountAndRejectsOverRefund(t *testing.T) {
	now := time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC)
	s := NewStripe(StripeConfig{WebhookSecret: "test-signing-secret", Now: func() time.Time { return now }})
	for _, amount := range []int64{250, 1000, 1001} {
		body := `{"id":"evt_refund","type":"charge.refunded","data":{"object":{"id":"ch_1","metadata":{"trade_no":"v3_1"},"amount":1000,"amount_refunded":` + strconv.FormatInt(amount, 10) + `,"currency":"usd"}}}`
		head := http.Header{"Stripe-Signature": {stripeSignature(s.cfg.WebhookSecret, now, body)}}
		event, err := s.Verify(context.Background(), head, []byte(body))
		if amount > 1000 {
			if err == nil {
				t.Fatal("over-refund accepted")
			}
			continue
		}
		if err != nil || !event.Refunded || event.AmountMinor != amount || event.TradeNo != "v3_1" {
			t.Fatalf("refund=%+v err=%v", event, err)
		}
	}
}

func TestCheckoutReturnURLRejectsUntrustedOrigins(t *testing.T) {
	s := New(nil, nil, nil, Config{ReturnOrigins: []string{"https://codego.example"}})
	for _, raw := range []string{"https://codego.example/path", "https://codego.example/success?q=1"} {
		if !s.allowedReturn(raw) {
			t.Fatalf("expected allowed: %s", raw)
		}
	}
	for _, raw := range []string{"https://codego.example.evil/path", "https://codego.example@evil.test/", "http://codego.example/path", "//codego.example/path", "javascript:alert(1)", "https://codego.example:444/path"} {
		if s.allowedReturn(raw) {
			t.Fatalf("untrusted return allowed: %s", raw)
		}
	}
}

func TestCommerceRoutesDenyUnauthenticatedAndNonAdmin(t *testing.T) {
	s := New(nil, nil, nil, Config{})
	for _, test := range []struct {
		name         string
		auth         Authenticate
		method, path string
		status       int
	}{
		{"login required", nil, "GET", "/api/commerce/orders", http.StatusUnauthorized},
		{"admin required", func(*http.Request) (Actor, error) { return Actor{UserID: 1, Role: "user"}, nil }, "GET", "/api/commerce/admin/orders", http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			mux := http.NewServeMux()
			s.Register(mux, test.auth)
			r := httptest.NewRequest(test.method, test.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("got %d want %d", w.Code, test.status)
			}
		})
	}
}

func TestBrowserPaymentReturnCannotChooseDestinationOrFulfill(t *testing.T) {
	s := New(nil, nil, nil, Config{ReturnOrigins: []string{"https://site.test"}})
	mux := http.NewServeMux()
	s.Register(mux, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/subscription/epay/return?return_url=https://evil.test&trade_status=TRADE_SUCCESS", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "https://site.test/orders" {
		t.Fatalf("untrusted browser return=%d %s", w.Code, w.Header().Get("Location"))
	}
}
