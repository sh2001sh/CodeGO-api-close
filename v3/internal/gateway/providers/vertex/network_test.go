package vertex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type pinnedFixture struct {
	t         *testing.T
	transport http.RoundTripper
}

func (p pinnedFixture) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("image TLS transport requires pinning")
}

func (p pinnedFixture) PublicImageTransport(host string, address netip.Addr) (http.RoundTripper, error) {
	if host != "8.8.8.8" || address != netip.MustParseAddr("8.8.8.8") {
		p.t.Error("image TLS transport did not receive the validated public address")
	}
	return p.transport, nil
}

func TestSelectedOAuthAndImageTransportsReceiveOriginalCredentialIdentity(t *testing.T) {
	c, _ := testAccount(t)
	c.TokenURI = "https://oauth.example/token"
	target := gateway.Target{ChannelID: 9, CredentialID: 42, BaseURL: "https://vertex.example", Secret: encoded(t, c),
		UpstreamModel: "gemini-2.5-pro", ProxyURL: "http://proxy.example:8080", Settings: map[string]any{"api_version": `{"alias":"us-east5"}`},
		Fingerprint: gateway.CredentialFingerprint{TLSProfile: "custom-tls", UserAgent: "credential-agent"}}
	checkTarget := func(selected gateway.Target) {
		t.Helper()
		if selected.ChannelID != target.ChannelID || selected.CredentialID != target.CredentialID || selected.Secret != target.Secret || selected.BaseURL != target.BaseURL ||
			selected.ProxyURL != target.ProxyURL || selected.Fingerprint != target.Fingerprint || selected.Settings["api_version"] != target.Settings["api_version"] {
			t.Error("network selector received rewritten credential/routing identity")
		}
	}
	var oauthCalls, imageCalls, clientSelections atomic.Int64
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("original-policy") },
		Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
			oauthCalls.Add(1)
			if r.URL.String() != c.TokenURI || r.Method != http.MethodPost || r.Header.Get("Authorization") != "" || r.ParseForm() != nil || r.Form.Get("assertion") == "" {
				t.Error("OAuth request bypassed selected transport or lost assertion")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"access_token":"selected-token","expires_in":3600}`))}, nil
		})}
	p := Provider{Clients: func(_ context.Context, selected gateway.Target) (*http.Client, error) {
		checkTarget(selected)
		clientSelections.Add(1)
		return client, nil
	}, ImageTransport: func(_ context.Context, selected gateway.Target) (http.RoundTripper, error) {
		checkTarget(selected)
		return pinnedFixture{t: t, transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
			imageCalls.Add(1)
			if r.URL.Host != "8.8.8.8" || r.Header.Get("Authorization") != "" || r.Header.Get("X-Goog-Api-Key") != "" || r.Header.Get("X-Goog-User-Project") != "" {
				t.Error("origin credential leaked to image source")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(strings.NewReader("\x89PNG\r\n\x1a\nactual bytes"))}, nil
		})}, nil
	}, HTTPClient: &http.Client{Transport: fixtureTransport(func(*http.Request) (*http.Response, error) {
		t.Error("selected OAuth client silently fell back to HTTPClient")
		return nil, errors.New("fallback")
	})}, tokens: &tokenCache{entries: make(map[[32]byte]*tokenEntry)}}
	const body = `{"model":"alias","messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"https://8.8.8.8/image?private-signature=do-not-log"}}]}]}`
	req := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "alias", Body: []byte(body)}
	for i := 0; i < 2; i++ {
		out, err := p.BuildRequest(context.Background(), req, target)
		if err != nil {
			t.Fatal(err)
		}
		converted, readErr := io.ReadAll(out.Body)
		_ = out.Body.Close()
		if readErr != nil || out.Header.Get("Authorization") != "Bearer selected-token" || !strings.Contains(out.URL.Path, "/locations/us-east5/") ||
			gjson.GetBytes(converted, "contents.0.parts.1.inlineData.mimeType").Str != "image/png" || gjson.GetBytes(converted, "contents.0.parts.1.inlineData.data").Str != "iVBORw0KGgphY3R1YWwgYnl0ZXM=" || string(req.Body) != body {
			t.Fatalf("selected transport conversion failed: %s %v", converted, readErr)
		}
	}
	if oauthCalls.Load() != 1 || clientSelections.Load() != 1 || imageCalls.Load() != 2 || client.Timeout != 2*time.Minute || client.CheckRedirect(nil, nil).Error() != "original-policy" || target.Settings["api_version"] != `{"alias":"us-east5"}` {
		t.Fatal("cache or delegation mutated shared client/settings")
	}
}

func TestSelectedOAuthClientErrorsAndRedirectsCannotFallBack(t *testing.T) {
	c, _ := testAccount(t)
	c.TokenURI = "https://oauth.example/token"
	for _, tc := range []struct {
		name   string
		client *http.Client
		err    error
	}{
		{"nil-client", nil, nil},
		{"selection-failure", nil, errors.New("PRIVATE-SECRET")},
		{"missing-custom-tls-transport", &http.Client{}, nil},
		{"cancelled", nil, context.Canceled},
		{"deadline", nil, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := Provider{Clients: func(context.Context, gateway.Target) (*http.Client, error) { return tc.client, tc.err }}
			_, _, err := p.exchangeToken(context.Background(), c, c.TokenURI, "", gateway.Target{Fingerprint: gateway.CredentialFingerprint{TLSProfile: "custom"}})
			if err == nil || strings.Contains(err.Error(), "PRIVATE-SECRET") {
				t.Fatalf("selected-client error disappeared or exposed secrets: %v", err)
			}
			if tc.err == context.Canceled || tc.err == context.DeadlineExceeded {
				if !errors.Is(err, tc.err) {
					t.Fatalf("lost cancellation: %v", err)
				}
			} else {
				var upstream *gateway.UpstreamError
				if !errors.As(err, &upstream) || upstream.Status != 502 {
					t.Fatalf("client configuration did not fail as 502: %v", err)
				}
			}
		})
	}
	var calls atomic.Int64
	selected := &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": {"https://redirect.example/token"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	p := Provider{Clients: func(context.Context, gateway.Target) (*http.Client, error) { return selected, nil }}
	_, _, err := p.exchangeToken(context.Background(), c, c.TokenURI, "")
	if err == nil || calls.Load() != 1 || !strings.Contains(err.Error(), "HTTP 307") || selected.CheckRedirect != nil || selected.Timeout != 0 {
		t.Fatalf("selected client followed redirect or mutated original: %v %d", err, calls.Load())
	}
}

func TestMissingOAuthTLSSelectorFailsClosed(t *testing.T) {
	c, _ := testAccount(t)
	_, _, err := (Provider{}).exchangeToken(context.Background(), c, "https://oauth.example/token", "", gateway.Target{Fingerprint: gateway.CredentialFingerprint{TLSProfile: "custom"}})
	var upstream *gateway.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 502 {
		t.Fatalf("custom TLS silently used default OAuth client: %v", err)
	}
}
