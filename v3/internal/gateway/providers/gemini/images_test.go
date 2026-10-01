package gemini_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/gemini"
	"github.com/tidwall/gjson"
)

const remoteChatImageBody = `{"messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"http://8.8.8.8/image?signature=private-query"}}]}]}`

func TestGeminiRemoteImageFetchConvertsActualBytesAndKeepsOriginalBody(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nimage payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/image" || r.Header.Get("X-Goog-Api-Key") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("bad image request or credential leak: %s %v", r.URL, r.Header)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer server.Close()
	selected := false
	provider := gemini.Provider{ImageTransport: func(_ context.Context, target gateway.Target) (http.RoundTripper, error) {
		selected = true
		if target.ChannelID != 4 || target.Secret != "upstream-key" || target.Fingerprint.TLSProfile != "chrome" {
			t.Error("channel policy did not reach image transport selector")
		}
		return &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "8.8.8.8:80" {
				t.Errorf("unvalidated image destination: %s", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		}}, nil
	}}
	req := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "gemini", Body: []byte(remoteChatImageBody)}
	upstream, err := provider.BuildRequest(context.Background(), req, gateway.Target{ChannelID: 4,
		BaseURL: "https://google.example", Secret: "upstream-key", Fingerprint: gateway.CredentialFingerprint{TLSProfile: "chrome"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(upstream.Body)
	if err != nil || !selected || string(req.Body) != remoteChatImageBody || gjson.GetBytes(got, "contents.0.parts.1.inlineData.mimeType").Str != "image/png" {
		t.Fatalf("remote image conversion changed source or failed: %s %v", got, err)
	}
	if gjson.GetBytes(got, "contents.0.parts.1.inlineData.data").Str != "iVBORw0KGgppbWFnZSBwYXlsb2Fk" {
		t.Fatalf("actual downloaded bytes lost: %s", got)
	}
}

func TestGeminiRemoteImageFetchFailureDoesNotLeakSignedURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) }))
	defer server.Close()
	provider := gemini.Provider{ImageTransport: func(context.Context, gateway.Target) (http.RoundTripper, error) {
		return &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		}}, nil
	}}
	_, err := provider.BuildRequest(context.Background(), &gateway.Request{Protocol: gateway.ProtocolOpenAIChat,
		Model: "gemini", Body: []byte(remoteChatImageBody)}, gateway.Target{BaseURL: "https://google.example"})
	var failure *gateway.UpstreamError
	if !errors.As(err, &failure) || failure.Status != 400 || strings.Contains(err.Error(), "private-query") {
		t.Fatalf("source fetch failure misclassified or leaked URL: %v", err)
	}
}

func TestGeminiRemoteImageHonorsCancellationAndRequiresTLSSelector(t *testing.T) {
	req := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "gemini", Body: []byte(remoteChatImageBody)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (gemini.Provider{}).BuildRequest(ctx, req, gateway.Target{BaseURL: "https://google.example"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("image fetch ignored parent cancellation: %v", err)
	}
	_, err := (gemini.Provider{}).BuildRequest(context.Background(), req, gateway.Target{BaseURL: "https://google.example",
		Fingerprint: gateway.CredentialFingerprint{TLSProfile: "chrome"}})
	var failure *gateway.UpstreamError
	if !errors.As(err, &failure) || failure.Status != 502 || failure.Code != "invalid_channel_configuration" {
		t.Fatalf("custom channel TLS silently discarded: %v", err)
	}
}
