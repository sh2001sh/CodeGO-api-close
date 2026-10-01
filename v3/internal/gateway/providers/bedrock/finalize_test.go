package bedrock

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

// Regression: channel overrides run after native conversion and must be bound
// by the final SigV4 signature, including the exact Content-Length.
func TestFinalizeSignsOverriddenRequest(t *testing.T) {
	original := `{"model":"claude","max_tokens":128,"messages":[{"role":"user","content":"hello"}]}`
	req := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "claude", Body: []byte(original)}
	target := gateway.Target{Secret: "test-id|test-secret|us-east-1", BaseURL: "https://aws.example.invalid",
		UpstreamModel: "anthropic.claude-v2", ParamOverride: map[string]any{"temperature": 0.7},
		HeaderOverride: map[string]string{"X-Trace": "request-one"}}
	out, err := (Provider{}).BuildRequest(context.Background(), req, target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()
	initialSignature := out.Header.Get("Authorization")
	if err := gateway.ApplyUpstreamRequest(out, req, target); err != nil {
		t.Fatal(err)
	}
	if err := (Provider{}).FinalizeRequest(context.Background(), out, req, target); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(body, "temperature").Float() != 0.7 || string(req.Body) != original || out.ContentLength != int64(len(body)) {
		t.Fatal("override or final body changed frozen client inputs")
	}
	if got, want := out.Header.Get("Authorization"), expectedOverriddenSignature(out, body); got != want || got == initialSignature {
		t.Fatalf("final signature does not bind transmitted bytes: got %s want %s", got, want)
	}
}

// This independent canonical computation uses the fixture's known headers and
// path rather than the production signer's header collection or URL encoder.
func expectedOverriddenSignature(out *http.Request, body []byte) string {
	timestamp := out.Header.Get("X-Amz-Date")
	date := timestamp[:8]
	names := "accept;content-length;content-type;host;x-amz-date;x-trace"
	headers := "accept:application/json\ncontent-length:" + strconv.Itoa(len(body)) +
		"\ncontent-type:application/json\nhost:aws.example.invalid\nx-amz-date:" + timestamp + "\nx-trace:request-one\n"
	canonical := "POST\n/model/anthropic.claude-v2/invoke\n\n" + headers + "\n" + names + "\n" + testHash(body)
	scope := date + "/us-east-1/bedrock/aws4_request"
	key := testHMAC([]byte("AWS4test-secret"), date)
	for _, part := range []string{"us-east-1", "bedrock", "aws4_request"} {
		key = testHMAC(key, part)
	}
	signature := hex.EncodeToString(testHMAC(key, "AWS4-HMAC-SHA256\n"+timestamp+"\n"+scope+"\n"+testHash([]byte(canonical))))
	return "AWS4-HMAC-SHA256 Credential=test-id/" + scope + ", SignedHeaders=" + names + ", Signature=" + signature
}

func testHash(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func testHMAC(key []byte, value string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(value))
	return h.Sum(nil)
}

func TestFinalizePreservesBearerOverrideAndCancellation(t *testing.T) {
	req := &gateway.Request{Protocol: gateway.ProtocolAnthropic, Model: "claude", Body: []byte(`{"messages":[]}`)}
	target := gateway.Target{Secret: "bearer|us-east-1"}
	out, err := (Provider{}).BuildRequest(context.Background(), req, target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()
	out.Header.Set("Authorization", "Bearer configured")
	if err := (Provider{}).FinalizeRequest(context.Background(), out, req, target); err != nil || out.Header.Get("Authorization") != "Bearer configured" {
		t.Fatalf("bearer override lost: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (Provider{}).FinalizeRequest(ctx, out, req, target); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled finalizer continued: %v", err)
	}
	target.Secret = "bad-secret-component"
	if err := (Provider{}).FinalizeRequest(context.Background(), out, req, target); err == nil || strings.Contains(err.Error(), target.Secret) {
		t.Fatalf("invalid credential accepted or leaked: %v", err)
	}
}

type finalBodyCloseError struct{ io.Reader }

func (finalBodyCloseError) Close() error { return errors.New("sensitive-reader-error") }

func TestFinalizeBodyFailureIsSafeAndUnreplayableBodyIsRestored(t *testing.T) {
	target := gateway.Target{Secret: "test-id|test-secret|us-east-1"}
	for _, test := range []struct {
		name string
		get  func() (io.ReadCloser, error)
	}{
		{"nil-reader", func() (io.ReadCloser, error) { return nil, nil }},
		{"get-error", func() (io.ReadCloser, error) { return nil, errors.New("sensitive-reader-error") }},
		{"close-error", func() (io.ReadCloser, error) { return finalBodyCloseError{strings.NewReader(`{}`)}, nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			out, err := http.NewRequest(http.MethodPost, "https://aws.example.invalid", strings.NewReader(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = out.Body.Close() }()
			out.GetBody = test.get
			if err := (Provider{}).FinalizeRequest(context.Background(), out, nil, target); err == nil || strings.Contains(err.Error(), "sensitive-reader-error") {
				t.Fatalf("body failure accepted or leaked: %v", err)
			}
		})
	}
	out, err := http.NewRequest(http.MethodPost, "https://aws.example.invalid", io.NopCloser(strings.NewReader(`{"x":1}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()
	out.Header.Set("Content-Length", "999")
	if err := (Provider{}).FinalizeRequest(context.Background(), out, nil, target); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(out.Body)
	if err != nil || string(body) != `{"x":1}` || out.ContentLength != 7 || out.Header.Get("Content-Length") != "" || out.GetBody == nil {
		t.Fatalf("finalizer consumed or misdeclared body: %s %v", body, err)
	}
}
