package boot

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestMarketBatchSignatureBindsJobKeyAndBodyWithoutConsumingRequest(t *testing.T) {
	secret := bytes.Repeat([]byte{9}, 32)
	id := "market-test-0123456789abcdef0123456789abcdef"
	body := `{"model":"fixture-model","messages":[]}`
	for _, change := range []string{"none", "job", "key", "body", "signature", "path", "method", "large"} {
		t.Run(change, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer fixture-secret")
			r.Header.Set("X-Market-Batch-Request-ID", id)
			r.Header.Set("X-Market-Batch-Signature", marketBatchSignature(secret, id, r.Header.Get("Authorization"), []byte(body)))
			wanted := body
			switch change {
			case "job":
				r.Header.Set("X-Market-Batch-Request-ID", "market-test-1123456789abcdef0123456789abcdef")
			case "key":
				r.Header.Set("Authorization", "Bearer other-secret")
			case "body":
				wanted = `{"model":"other"}`
				r.Body = io.NopCloser(strings.NewReader(wanted))
			case "signature":
				r.Header.Set("X-Market-Batch-Signature", strings.Repeat("0", 64))
			case "path":
				r.URL.Path = "/v1/responses"
			case "method":
				r.Method = http.MethodGet
			case "large":
				wanted = strings.Repeat("a", (1<<20)+3)
				r.Body = io.NopCloser(strings.NewReader(wanted))
			}
			got := MarketBatchRequestID(r, secret)
			if (change == "none" && got != id) || (change != "none" && got != "") {
				t.Fatalf("signature policy %s => %s", change, got)
			}
			remaining, err := io.ReadAll(r.Body)
			if err != nil || string(remaining) != wanted {
				t.Fatal("validation consumed or altered relay body")
			}
		})
	}
}

func TestMarketBatchUnavailableConfigurationIsExplicit(t *testing.T) {
	for _, raw := range []string{"", "ftp://gateway", "http://user:secret@gateway", "http://gateway/path", "http://gateway?key=secret"} {
		if _, err := NewMarketBatchRelay(nil, nil, MarketBatchConfig{GatewayBaseURL: raw, SigningKey: bytes.Repeat([]byte{9}, 32)}); !errors.Is(err, channelmarket.ErrUnavailable) || strings.Contains(err.Error(), "secret") {
			t.Fatalf("configuration %q: %v", raw, err)
		}
	}
	if _, err := NewMarketBatchIdentity(nil, "invalid", nil); !errors.Is(err, channelmarket.ErrUnavailable) {
		t.Fatalf("key identity configuration: %v", err)
	}
}
