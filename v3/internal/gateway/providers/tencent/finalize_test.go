package tencent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func finalize(t *testing.T, ctx context.Context, out *http.Request, req *gateway.Request, target gateway.Target) error {
	t.Helper()
	provider, ok := any(Provider{}).(interface {
		FinalizeRequest(context.Context, *http.Request, *gateway.Request, gateway.Target) error
	})
	if !ok {
		t.Fatal("Tencent lacks final signing hook after channel overrides")
	}
	return provider.FinalizeRequest(ctx, out, req, target)
}

func TestFinalSignatureMatchesIndependentFixtureAfterNativeOverrides(t *testing.T) {
	req := fixtureRequest(`{"messages":[{"role":"user","content":"hello"}],"temperature":0.5}`, false)
	target := gateway.Target{Secret: "123|test-id|test-secret", UpstreamModel: "hunyuan-lite", ParamOverride: map[string]any{"Temperature": 0.25},
		HeaderOverride: map[string]string{"Content-Type": "application/json; charset=utf-8", "X-TC-Action": "OtherAction", "X-TC-Timestamp": "1700000000", "Host": "signing.example.test"}}
	out, err := (Provider{}).BuildRequest(context.Background(), req, target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()
	old := out.Header.Get("Authorization")
	if err := gateway.ApplyUpstreamRequest(out, req, target); err != nil {
		t.Fatal(err)
	}
	if err := finalize(t, context.Background(), out, req, target); err != nil {
		t.Fatal(err)
	}
	want := "TC3-HMAC-SHA256 Credential=test-id/2023-11-14/hunyuan/tc3_request, SignedHeaders=content-type;host;x-tc-action, Signature=d6b033648d2abe0dc83908ca2a6769e7a1100ce66c945e9114c1d6c5202f80cf"
	body, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatal(err)
	}
	expectedBody := `{"Model":"hunyuan-lite","Messages":[{"Role":"user","Content":"hello"}],"Stream":false,"Temperature":0.25}`
	if string(body) != expectedBody || out.Header.Get("Authorization") != want || old == want || out.Host != "signing.example.test" {
		t.Fatalf("final bytes/headers did not match independent .NET signing vector: %s", body)
	}
}

func TestFinalizerRestoresNonReplayableBodyAndOriginalTimestamp(t *testing.T) {
	req := fixtureRequest(`{"messages":[{"role":"user","content":"hello"}]}`, false)
	target := gateway.Target{Secret: "123|test-id|test-secret"}
	out, err := (Provider{}).BuildRequest(context.Background(), req, target)
	if err != nil {
		t.Fatal(err)
	}
	originalTimestamp := out.Header.Get("X-TC-Timestamp")
	_ = out.Body.Close()
	body := `{"Messages":[{"Role":"user","Content":"final bytes"}],"Model":"native","Stream":false}`
	out.Body = io.NopCloser(strings.NewReader(body))
	out.GetBody = nil
	out.ContentLength = -1
	out.Header.Del("X-TC-Timestamp")
	out.Header.Set("Content-Length", "999")
	if err := finalize(t, context.Background(), out, req, target); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()
	actual, _ := io.ReadAll(out.Body)
	if string(actual) != body || out.GetBody == nil || out.ContentLength != int64(len(body)) || out.Header.Get("Content-Length") != "" || out.Header.Get("X-TC-Timestamp") != originalTimestamp {
		t.Fatal("finalizer consumed transport body or lost builder timestamp")
	}
	clone, err := out.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	copyBody, _ := io.ReadAll(clone)
	_ = clone.Close()
	if string(copyBody) != body {
		t.Fatal("GetBody does not replay signed bytes")
	}
}

func TestFinalizerRejectsInvalidTimestampSafely(t *testing.T) {
	for _, timestamp := range []string{"sensitive-value", "-1", "9223372036854775808"} {
		req := fixtureRequest(`{"messages":[{"role":"user","content":"hello"}]}`, false)
		target := gateway.Target{Secret: "123|test-id|test-secret"}
		out, err := (Provider{}).BuildRequest(context.Background(), req, target)
		if err != nil {
			t.Fatal(err)
		}
		out.Header.Set("X-TC-Timestamp", timestamp)
		err = finalize(t, context.Background(), out, req, target)
		_ = out.Body.Close()
		var failure *gateway.UpstreamError
		if !errors.As(err, &failure) || failure.Status != http.StatusBadGateway || strings.Contains(err.Error(), "sensitive") {
			t.Fatalf("invalid final timestamp accepted or leaked: %v", err)
		}
	}
}

func TestFinalizerContextCancellationPreventsSigning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := finalize(t, ctx, nil, nil, gateway.Target{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}

func TestFinalizerRetainsTC3AuthenticationAndRequiredHeaders(t *testing.T) {
	req := fixtureRequest(`{"messages":[{"role":"user","content":"hello"}]}`, false)
	target := gateway.Target{Secret: "123|test-id|test-secret"}
	out, err := (Provider{}).BuildRequest(context.Background(), req, target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()
	out.Header.Set("Authorization", "Bearer custom-header")
	for _, header := range []string{"Content-Type", "X-TC-Action", "X-TC-Version"} {
		out.Header.Del(header)
	}
	if err := finalize(t, context.Background(), out, req, target); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.Header.Get("Authorization"), "TC3-HMAC-SHA256 Credential=test-id/") || out.Header.Get("Content-Type") != "application/json" || out.Header.Get("X-TC-Action") != "ChatCompletions" || out.Header.Get("X-TC-Version") != "2023-09-01" {
		t.Fatal("native TC3 authentication or required headers lost")
	}
}

func TestFinalizerBodyFailuresDoNotExposeReaderDetails(t *testing.T) {
	req := fixtureRequest(`{"messages":[{"role":"user","content":"hello"}]}`, false)
	target := gateway.Target{Secret: "123|test-id|test-secret"}
	out, err := (Provider{}).BuildRequest(context.Background(), req, target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()
	out.GetBody = func() (io.ReadCloser, error) { return nil, errors.New("test-secret in storage error") }
	err = finalize(t, context.Background(), out, req, target)
	var failure *gateway.UpstreamError
	if !errors.As(err, &failure) || failure.Status != http.StatusBadGateway || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("body failure missing or leaked: %v", err)
	}
}
