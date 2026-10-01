package bedrock

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type imagePinnedTransport struct{ t *testing.T }

func (p imagePinnedTransport) PublicImageTransport(host string, address netip.Addr) (http.RoundTripper, error) {
	if host != "8.8.8.8" || address != netip.MustParseAddr("8.8.8.8") {
		p.t.Fatalf("image address was not pinned: %s %s", host, address)
	}
	return p, nil
}

func (p imagePinnedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || req.Method != http.MethodGet {
		p.t.Fatal("image request inherited upstream credentials or body")
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/png"}},
		Body: io.NopCloser(bytes.NewReader([]byte("\x89PNG\r\n\x1a\n"))), Request: req}, nil
}

func TestRemoteImagesAreInlinedForClaudeAndNova(t *testing.T) {
	for _, test := range []struct {
		name     string
		protocol gateway.Protocol
		model    string
		content  string
		path     string
	}{
		{"chat-claude", gateway.ProtocolOpenAIChat, "claude", `[{"type":"image_url","image_url":{"url":"https://8.8.8.8/image.png"}}]`, "messages.0.content.0.source.data"},
		{"native-claude", gateway.ProtocolAnthropic, "claude", `[{"type":"image","source":{"type":"url","url":"https://8.8.8.8/image.png"}}]`, "messages.0.content.0.source.data"},
		{"chat-nova", gateway.ProtocolOpenAIChat, "amazon.nova-pro-v1:0", `[{"type":"image_url","image_url":{"url":"https://8.8.8.8/image.png"}}]`, "messages.0.content.0.image.source.bytes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := `{"model":"` + test.model + `","max_tokens":128,"messages":[{"role":"user","content":` + test.content + `}]}`
			req := &gateway.Request{Protocol: test.protocol, Model: test.model, Body: []byte(original)}
			target := gateway.Target{Secret: "bearer|us-east-1", ProxyURL: "https://proxy.example.invalid", Fingerprint: gateway.CredentialFingerprint{TLSProfile: "chrome"}}
			p := Provider{ImageTransport: func(_ context.Context, selected gateway.Target) (http.RoundTripper, error) {
				if selected.ProxyURL != target.ProxyURL || selected.Fingerprint.TLSProfile != "chrome" {
					t.Fatal("image selector lost channel network policy")
				}
				return imagePinnedTransport{t}, nil
			}}
			out, err := p.BuildRequest(context.Background(), req, target)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = out.Body.Close() }()
			body, err := io.ReadAll(out.Body)
			if err != nil || gjson.GetBytes(body, test.path).Str != base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n")) {
				t.Fatalf("remote image not inlined in native request: %s %v", body, err)
			}
			if string(req.Body) != original || strings.Contains(string(body), "8.8.8.8") {
				t.Fatal("image rewrite changed original billing input or retained remote URL")
			}
		})
	}
}

func TestRemoteImagesRejectPrivateTargetsAndRetainCancellation(t *testing.T) {
	req := &gateway.Request{Protocol: gateway.ProtocolAnthropic, Model: "claude", Body: []byte(`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"http://127.0.0.1/private"}}]}]}`)}
	target := gateway.Target{Secret: "bearer|us-east-1"}
	_, err := (Provider{}).BuildRequest(context.Background(), req, target)
	var upstream *gateway.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != http.StatusBadRequest || upstream.Code != "image_fetch_failed" {
		t.Fatalf("private image URL was not rejected: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Provider{}).BuildRequest(ctx, req, target); !errors.Is(err, context.Canceled) {
		t.Fatalf("image conversion lost cancellation: %v", err)
	}
	target.Fingerprint.TLSProfile = "chrome"
	if _, err := (Provider{}).BuildRequest(context.Background(), req, target); !errors.As(err, &upstream) || upstream.Status != http.StatusBadGateway {
		t.Fatalf("unsupported network identity silently ignored: %v", err)
	}
}
