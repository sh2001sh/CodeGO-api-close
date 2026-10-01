package catalogcontrol

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestLegacyProbeSignsFinalNativeOverridesAndBlocksInvalidSigning(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"Temperature":0.25`) || r.Host != "signing.example.test" || r.Header.Get("Authorization") != probeTC3Signature(body) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, `{"Response":{"Choices":[{"Message":{"Role":"assistant","Content":"OK"},"FinishReason":"stop"}],"Usage":{"PromptTokens":3,"CompletionTokens":2}}}`)
	}))
	defer upstream.Close()
	target := gateway.Target{Provider: "tencent", BaseURL: upstream.URL, Secret: "123|test-id|test-secret", UpstreamModel: "hunyuan-lite",
		ParamOverride: map[string]any{"Temperature": 0.25}, HeaderOverride: map[string]string{
			"Content-Type": "application/json; charset=utf-8", "X-TC-Action": "OtherAction", "X-TC-Timestamp": "1700000000", "Host": "signing.example.test"}}
	if err := runChannelProbe(context.Background(), target, "hunyuan-lite", "", false); err != nil {
		t.Fatalf("probe did not sign final converted bytes: %v", err)
	}
	target.HeaderOverride["X-TC-Timestamp"] = "invalid"
	if err := runChannelProbe(context.Background(), target, "hunyuan-lite", "", false); err == nil || calls.Load() != 1 || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("invalid signing config contacted upstream or leaked credential: %v calls=%d", err, calls.Load())
	}
}

// Independently implement Tencent's published TC3 wire verification for a
// fixed date and final host/action headers, including the exact sent body.
func probeTC3Signature(body []byte) string {
	hash := func(value []byte) string {
		h := sha256.Sum256(value)
		return hex.EncodeToString(h[:])
	}
	sign := func(key []byte, value string) []byte {
		h := hmac.New(sha256.New, key)
		_, _ = h.Write([]byte(value))
		return h.Sum(nil)
	}
	canonical := "POST\n/\n\ncontent-type:application/json; charset=utf-8\nhost:signing.example.test\nx-tc-action:otheraction\n\ncontent-type;host;x-tc-action\n" + hash(body)
	scope := "2023-11-14/hunyuan/tc3_request"
	toSign := "TC3-HMAC-SHA256\n1700000000\n" + scope + "\n" + hash([]byte(canonical))
	key := sign(sign(sign([]byte("TC3test-secret"), "2023-11-14"), "hunyuan"), "tc3_request")
	return "TC3-HMAC-SHA256 Credential=test-id/" + scope + ", SignedHeaders=content-type;host;x-tc-action, Signature=" + hex.EncodeToString(sign(key, toSign))
}
