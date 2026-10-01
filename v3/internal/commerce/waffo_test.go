package commerce

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func waffoTestKeys(t *testing.T) (*rsa.PrivateKey, string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), base64.StdEncoding.EncodeToString(public)
}

func waffoTestSign(t *testing.T, key *rsa.PrivateKey, body string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(body))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func waffoTestVerify(t *testing.T, key *rsa.PublicKey, body []byte, sig string) {
	t.Helper()
	digest := sha256.Sum256(body)
	decoded, err := base64.StdEncoding.DecodeString(sig)
	if err != nil || rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], decoded) != nil {
		t.Fatal("invalid outbound RSA signature")
	}
}

func TestWaffoCheckoutSignsExactPriceAndValidatesProviderResponse(t *testing.T) {
	merchant, private, _ := waffoTestKeys(t)
	provider, _, public := waffoTestKeys(t)
	var badResponse atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		waffoTestVerify(t, &merchant.PublicKey, body, r.Header.Get("X-SIGNATURE"))
		var fields map[string]any
		if err := json.Unmarshal(body, &fields); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/order/create" || fields["paymentRequestId"] != "v3_w1" || fields["merchantOrderId"] != "v3_w1" || fields["orderAmount"] != "12.50" || fields["orderCurrency"] != "USD" || r.Header.Get("X-API-KEY") != "test-api-key" || r.Header.Get("Idempotency-Key") != "v3_w1" {
			t.Errorf("request mismatch: path=%s fields=%+v", r.URL.Path, fields)
		}
		body = []byte(`{"code":"0","data":{"paymentRequestId":"v3_w1","merchantOrderId":"v3_w1","orderAction":"{\"webUrl\":\"https://waffo.test/pay\"}"}}`)
		if badResponse.Load() {
			w.Header().Set("X-SIGNATURE", waffoTestSign(t, provider, "tampered"))
		} else {
			w.Header().Set("X-SIGNATURE", waffoTestSign(t, provider, string(body)))
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	p := NewWaffo(WaffoConfig{APIKey: "test-api-key", MerchantID: "m1", PrivateKey: private, PublicKey: public, NotifyURL: "https://site.test/api/waffo/webhook", BaseURL: server.URL, Client: server.Client()})
	o := Order{TradeNo: "v3_w1", AmountMinor: 1250, Currency: "usd", Kind: "topup", UserID: 3}
	checkout, err := p.Checkout(context.Background(), o, "https://site.test/success", "https://site.test/cancel")
	if err != nil || checkout != (Checkout{Reference: "v3_w1", URL: "https://waffo.test/pay"}) {
		t.Fatalf("checkout=%+v err=%v", checkout, err)
	}
	badResponse.Store(true)
	if _, err := p.Checkout(context.Background(), o, "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered response accepted: %v", err)
	}
	o.Currency = "cny"
	if _, err := p.Checkout(context.Background(), o, "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong currency accepted: %v", err)
	}
}

func TestWaffoWebhookRejectsTamperedAmountAndWrongMerchant(t *testing.T) {
	provider, _, public := waffoTestKeys(t)
	_, private, _ := waffoTestKeys(t)
	p := NewWaffo(WaffoConfig{MerchantID: "m1", PrivateKey: private, PublicKey: public})
	body := `{"eventType":"PAYMENT_NOTIFICATION","result":{"paymentRequestId":"v3_w1","merchantOrderId":"v3_w1","orderStatus":"PAY_SUCCESS","orderCurrency":"USD","orderAmount":"12.50","merchantInfo":{"merchantId":"m1"}}}`
	header := http.Header{}
	header.Set("X-SIGNATURE", waffoTestSign(t, provider, body))
	event, err := p.Verify(context.Background(), header, []byte(body))
	if err != nil || event != (PaymentEvent{ID: "payment:v3_w1", TradeNo: "v3_w1", Reference: "v3_w1", AmountMinor: 1250, Currency: "usd", Paid: true}) {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	if _, err := p.Verify(context.Background(), header, []byte(strings.Replace(body, "12.50", "0.01", 1))); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered amount accepted: %v", err)
	}
	for _, mutation := range []struct {
		old, next string
		want      error
	}{
		{"m1", "m2", ErrPaymentMismatch},
		{"12.50", "12.501", ErrInvalid},
		{"PAY_SUCCESS", "PAY_IN_PROGRESS", ErrIgnoredEvent},
		{"USD", "CNY", ErrInvalid},
	} {
		changed := strings.Replace(body, mutation.old, mutation.next, 1)
		header.Set("X-SIGNATURE", waffoTestSign(t, provider, changed))
		if _, err := p.Verify(context.Background(), header, []byte(changed)); !errors.Is(err, mutation.want) {
			t.Errorf("%s: err=%v want=%v", mutation.next, err, mutation.want)
		}
	}
	header, ack, err := p.WebhookResponse(true)
	if err != nil || string(ack) != `{"message":"success"}` || header.Get("Content-Type") != "application/json" {
		t.Fatalf("ack=%s err=%v", ack, err)
	}
	merchantKey, err := waffoPrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	waffoTestVerify(t, &merchantKey.PublicKey, ack, header.Get("X-SIGNATURE"))
}

func TestWaffoKeysAcceptExistingDERAndPEMFormatsAndRejectWrongTypes(t *testing.T) {
	key, private, public := waffoTestKeys(t)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{private, strings.ReplaceAll(private, "\n", `\n`), base64.StdEncoding.EncodeToString(der), string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))} {
		parsed, err := waffoPrivateKey(raw)
		if err != nil || parsed.N.Cmp(key.N) != 0 {
			t.Fatalf("private key parse failed: %v", err)
		}
	}
	for _, raw := range []string{public, string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)}))} {
		parsed, err := waffoPublicKey(raw)
		if err != nil || parsed.N.Cmp(key.N) != 0 {
			t.Fatalf("public key parse failed: %v", err)
		}
	}
	for _, invalid := range []string{"", "bad-key", public, private + private} {
		if _, err := waffoPrivateKey(invalid); !errors.Is(err, ErrProviderUnavailable) {
			t.Fatalf("invalid private key accepted: %v", err)
		}
	}
	if _, err := waffoPublicKey(private); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("private key used as public key: %v", err)
	}
}
