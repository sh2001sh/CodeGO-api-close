package native

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type pinnedMediaFixture struct {
	t        *testing.T
	requests int
	pins     int
	idle     int
}

func (p *pinnedMediaFixture) RoundTrip(req *http.Request) (*http.Response, error) {
	p.t.Fatal("unpinned credential transport was used for a public result URL")
	return nil, errors.New("unpinned transport")
}

func (p *pinnedMediaFixture) PublicImageTransport(host string, address netip.Addr) (http.RoundTripper, error) {
	p.pins++
	if host != "8.8.8.8" || address != netip.MustParseAddr("8.8.8.8") {
		p.t.Fatal("public media address was not pinned")
	}
	return pinnedMediaWire{p}, nil
}

type pinnedMediaWire struct{ fixture *pinnedMediaFixture }

func (p pinnedMediaWire) RoundTrip(req *http.Request) (*http.Response, error) {
	p.fixture.requests++
	if req.URL.Host != "8.8.8.8" || len(req.Header) != 0 || req.Method != http.MethodGet {
		p.fixture.t.Errorf("provider credentials/overrides crossed media boundary: %v", req)
	}
	return &http.Response{StatusCode: 429, Header: http.Header{"Content-Type": {"video/mp4"}}, Body: io.NopCloser(strings.NewReader("pinned-video")), Request: req}, nil
}

func (p pinnedMediaWire) CloseIdleConnections() { p.fixture.idle++ }

func TestPublicMediaKeepsSelectedIdentityThroughPinnedTransport(t *testing.T) {
	fixture := &pinnedMediaFixture{t: t}
	target := gateway.Target{ChannelID: 11, CredentialID: 12, BaseURL: "https://provider.invalid", ProxyURL: "http://configured-proxy.invalid", Secret: "provider-fixture",
		Fingerprint: gateway.CredentialFingerprint{UserAgent: "stable-media", TLSProfile: "firefox"}, HeaderOverride: map[string]string{"Authorization": "Bearer {api_key}", "X-Account": "private"},
		ParamOverride: map[string]any{"body_only": true}, StatusCodeMapping: map[string]int{"429": 200}}
	selections := 0
	clients := func(_ context.Context, selected gateway.Target) (*http.Client, error) {
		selections++
		if selected.CredentialID != 12 || selected.ProxyURL != target.ProxyURL || selected.Fingerprint != target.Fingerprint {
			t.Fatal("public media lost selected proxy/TLS/fingerprint")
		}
		return &http.Client{Transport: fixture}, nil
	}
	ctx := WithRequest(context.Background(), &gateway.Request{Model: "video"}, target, clients)
	response, err := Media(ctx, nil, target, "https://8.8.8.8/video?signature=fixture", http.Header{"Authorization": {"Bearer private"}})
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if response.StatusCode != 200 || string(body) != "pinned-video" || readErr != nil || closeErr != nil || selections != 1 || fixture.pins != 1 || fixture.requests != 1 || fixture.idle != 1 {
		t.Fatalf("pinned media response=%q status=%d selected=%d fixture=%+v read=%v close=%v", body, response.StatusCode, selections, fixture, readErr, closeErr)
	}
}

func TestPublicMediaRejectsUnpinnableOrMissingSelectedClients(t *testing.T) {
	for _, selected := range []*http.Client{nil, {Transport: identityTransport{}}} {
		clients := func(context.Context, gateway.Target) (*http.Client, error) { return selected, nil }
		ctx := WithRequest(context.Background(), &gateway.Request{}, gateway.Target{}, clients)
		response, err := Media(ctx, nil, gateway.Target{}, "https://8.8.8.8/video", nil)
		if err == nil || response != nil {
			t.Fatal("public media silently lost configured credential identity")
		}
	}
}
