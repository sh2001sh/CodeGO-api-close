package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaffoPancakeCheckoutSignsCanonicalRequestAndStoresOrderReference(t *testing.T) {
	merchant, private, public := waffoTestKeys(t)
	now := time.Unix(1800000000, 0)
	var missingReference atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		digest := sha256.Sum256(body)
		canonical := "POST\n/v1/actions/checkout/create-session\n1800000000\n" + base64.StdEncoding.EncodeToString(digest[:])
		waffoTestVerify(t, &merchant.PublicKey, []byte(canonical), r.Header.Get("X-Signature"))
		var fields struct {
			StoreID   string `json:"storeId"`
			ProductID string `json:"productId"`
			Currency  string `json:"currency"`
			Email     string `json:"buyerEmail"`
			Expires   int    `json:"expiresInSeconds"`
			Price     struct {
				Amount      string `json:"amount"`
				TaxIncluded bool   `json:"taxIncluded"`
			} `json:"priceSnapshot"`
		}
		if err := json.Unmarshal(body, &fields); err != nil {
			t.Error(err)
		}
		if fields.StoreID != "store_1" || fields.ProductID != "prod_1" || fields.Currency != "USD" || fields.Price.Amount != "12.50" || !fields.Price.TaxIncluded || fields.Expires != 60 || fields.Email != "buyer@example.test" || r.Header.Get("X-Environment") != "prod" || r.Header.Get("X-Merchant-Id") != "m1" || r.Header.Get("Idempotency-Key") != "v3_pc1" {
			t.Errorf("unexpected checkout fields: %+v", fields)
		}
		if missingReference.Load() {
			_, _ = w.Write([]byte(`{"data":{"sessionId":"session_1","checkoutUrl":"https://pancake.test/pay"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"sessionId":"session_1","orderId":"order_1","checkoutUrl":"https://pancake.test/pay"}}`))
	}))
	defer server.Close()
	p := NewWaffoPancake(WaffoPancakeConfig{MerchantID: "m1", PrivateKey: private, WebhookPublicKey: public, StoreID: "store_1", ProductID: "prod_1", BaseURL: server.URL, Client: server.Client(), Now: func() time.Time { return now }, BuyerEmail: func(context.Context, int64) (string, error) { return "buyer@example.test", nil }})
	o := Order{TradeNo: "v3_pc1", Currency: "usd", AmountMinor: 1250, ExpiresAt: now.Add(time.Minute)}
	checkout, err := p.Checkout(context.Background(), o, "https://site.test/success", "")
	if err != nil || checkout != (Checkout{Reference: "order_1", URL: "https://pancake.test/pay"}) {
		t.Fatalf("checkout=%+v err=%v", checkout, err)
	}
	missingReference.Store(true)
	if _, err := p.Checkout(context.Background(), o, "", ""); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("missing callback reference accepted: %v", err)
	}
	o.ExpiresAt = now
	if _, err := p.Checkout(context.Background(), o, "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expired order accepted: %v", err)
	}
}

func TestWaffoPancakeWebhookChecksRawBodyModeStoreFreshnessAndExactAmount(t *testing.T) {
	provider, _, public := waffoTestKeys(t)
	now := time.Unix(1800000000, 0)
	p := NewWaffoPancake(WaffoPancakeConfig{WebhookPublicKey: public, StoreID: "store_1", Now: func() time.Time { return now }})
	body := `{"id":"evt_1","eventType":"order.completed","storeId":"store_1","mode":"prod","data":{"orderId":"order_1","amount":"12.50","currency":"USD"}}`
	signed := func(body string, stamp time.Time) http.Header {
		header := http.Header{}
		timestamp := strconv.FormatInt(stamp.UnixMilli(), 10)
		header.Set("X-Waffo-Signature", "t="+timestamp+",v1="+waffoTestSign(t, provider, timestamp+"."+body))
		return header
	}
	header := signed(body, now)
	event, err := p.Verify(context.Background(), header, []byte(body))
	if err != nil || event != (PaymentEvent{ID: "evt_1", Reference: "order_1", AmountMinor: 1250, Currency: "usd", Paid: true}) {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	if _, err := p.Verify(context.Background(), header, []byte(strings.Replace(body, "12.50", "0.01", 1))); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered amount accepted: %v", err)
	}
	for _, mutation := range []struct {
		old, next string
		want      error
	}{
		{"prod", "test", ErrPaymentMismatch},
		{"store_1", "store_2", ErrPaymentMismatch},
		{"12.50", "12.501", ErrInvalid},
		{"USD", "CNY", ErrInvalid},
		{"order.completed", "order.failed", ErrIgnoredEvent},
		{"order_1", "", ErrInvalid},
		{"evt_1", "", ErrInvalid},
	} {
		changed := strings.Replace(body, mutation.old, mutation.next, 1)
		if _, err := p.Verify(context.Background(), signed(changed, now), []byte(changed)); !errors.Is(err, mutation.want) {
			t.Errorf("%s: err=%v want=%v", mutation.next, err, mutation.want)
		}
	}
	for _, stamp := range []time.Time{now.Add(-6 * time.Minute), now.Add(6 * time.Minute)} {
		if _, err := p.Verify(context.Background(), signed(body, stamp), []byte(body)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("out-of-window callback accepted: %v", err)
		}
	}
	header.Add("X-Waffo-Signature", header.Get("X-Waffo-Signature"))
	// Duplicate parameters in the actual signature value cannot override the
	// signed timestamp and bypass replay protection.
	header.Set("X-Waffo-Signature", header.Get("X-Waffo-Signature")+",t=1800000000000")
	if _, err := p.Verify(context.Background(), header, []byte(body)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate timestamp accepted: %v", err)
	}
	numeric := strings.Replace(body, `"12.50"`, "12.50", 1)
	if event, err := p.Verify(context.Background(), signed(numeric, now), []byte(numeric)); err != nil || event.AmountMinor != 1250 {
		t.Fatalf("numeric amount=%+v err=%v", event, err)
	}
	if _, err := p.Verify(context.Background(), signed("not JSON", now), []byte("not JSON")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("signed malformed JSON accepted: %v", err)
	}
}
