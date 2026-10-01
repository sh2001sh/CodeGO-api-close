package native

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestMediaRejectsUntrustedPrivateTargetsAndUnsafeSchemes(t *testing.T) {
	for _, raw := range []string{"http://example.com/video", "https://127.0.0.1/video", "https://[::1]/video", "file:///etc/passwd", "https://user:password@example.com/video"} {
		if response, err := Media(context.Background(), nil, gateway.Target{}, raw, nil); err == nil {
			if err := response.Body.Close(); err != nil {
				t.Error(err)
			}
			t.Fatalf("unsafe media allowed %s", raw)
		}
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.0.1", "100.64.0.1", "::ffff:127.0.0.1", "fc00::1", "169.254.169.254", "64:ff9b::7f00:1", "2002:7f00:1::", "0.1.2.3", "240.0.0.1"} {
		if publicIP(netip.MustParseAddr(raw)) {
			t.Fatalf("private address accepted %s", raw)
		}
	}
	if !publicIP(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public address rejected")
	}
}

func TestMediaTrustsOnlyConfiguredEndpointAndDeniesRedirect(t *testing.T) {
	var leaked bool
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer foreign.Close()
	trusted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, foreign.URL, http.StatusTemporaryRedirect)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Errorf("provider auth lost")
		}
		if _, err := w.Write([]byte("video")); err != nil {
			t.Error(err)
		}
	}))
	defer trusted.Close()
	target := gateway.Target{BaseURL: trusted.URL}
	response, err := Media(context.Background(), nil, target, trusted.URL+"/video", http.Header{"Authorization": {"Bearer fixture-token"}})
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("trusted media %v %v", response, err)
	}
	if err := response.Body.Close(); err != nil {
		t.Error(err)
	}
	response, err = Media(context.Background(), nil, target, trusted.URL+"/redirect", nil)
	if err != nil || response.StatusCode != 307 || leaked {
		t.Fatalf("redirect allowed %v %v leaked=%v", response, err, leaked)
	}
	if err := response.Body.Close(); err != nil {
		t.Error(err)
	}
}

func TestMediaSelectedTransportIsRestrictedToTrustedProviderHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" || r.Header.Get("User-Agent") != "stable-media-identity" {
			t.Errorf("content identity/header override missing: %v", r.Header)
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("video"))
	}))
	defer server.Close()
	target := gateway.Target{BaseURL: server.URL, Secret: "fixture", Fingerprint: gateway.CredentialFingerprint{UserAgent: "stable-media-identity"},
		ParamOverride: map[string]any{"body_only": true}, HeaderOverride: map[string]string{"Authorization": "Bearer {api_key}"}, StatusCodeMapping: map[string]int{"429": 200}}
	selections := 0
	clients := func(_ context.Context, selected gateway.Target) (*http.Client, error) {
		selections++
		return &http.Client{Transport: identityTransport{selected}}, nil
	}
	ctx := WithRequest(context.Background(), &gateway.Request{Model: "video"}, target, clients)
	resp, err := Media(ctx, nil, target, server.URL+"/video", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || selections != 1 {
		t.Fatalf("trusted media status/selected transport %d %d", resp.StatusCode, selections)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	resp, err = Media(ctx, nil, target, "https://127.0.0.1/video", nil)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("external private URL bypassed SSRF guard")
	}
	if selections != 1 {
		t.Fatal("untrusted result URL selected credential transport")
	}
}
