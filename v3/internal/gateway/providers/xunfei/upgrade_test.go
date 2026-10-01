package xunfei

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/bridge"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

type selectedRoundTripper func(*http.Request) (*http.Response, error)

func (f selectedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestSelectedNativeTransportUsesFallbackUpgradeAndRetainsSignedQueryHeadersAndPayload(t *testing.T) {
	server := nativeServer(t, func(conn *websocket.Conn) {
		var payload []byte
		if websocket.Message.Receive(conn, &payload) != nil {
			return
		}
		if gjson.GetBytes(payload, "header.app_id").Str != "test-app" || gjson.GetBytes(payload, "payload.message.text.0.content").Str != "hello" {
			t.Errorf("native payload changed: %s", payload)
		}
		_ = websocket.Message.Send(conn, nativeFrame(2, 0, "native reply", `{"prompt_tokens":7,"completion_tokens":2}`))
	})
	for _, stream := range []bool{false, true} {
		req, out := transportRequest(t, context.Background(), server.URL, stream)
		out.Header.Set("Authorization", "native-upstream-header")
		out.Header.Set("X-Configured-Header", "retained")
		out.Header.Set("Cookie", "upstream=retained")
		signed := out.URL.RawQuery
		var calls atomic.Int32
		fallback := selectedRoundTripper(func(upgrade *http.Request) (*http.Response, error) {
			calls.Add(1)
			if upgrade.Method != http.MethodGet || upgrade.URL.RawQuery != signed || upgrade.Header.Get("Authorization") != "native-upstream-header" || upgrade.Header.Get("X-Configured-Header") != "retained" || upgrade.Header.Get("Cookie") != "upstream=retained" || upgrade.Header.Get("Upgrade") != "websocket" {
				t.Error("selected handshake lost signed query or final headers")
			}
			if upgrade.Body != nil || upgrade.GetBody != nil || upgrade.ContentLength != 0 || upgrade.Header.Get("Content-Length") != "" || upgrade.RequestURI != "" {
				t.Error("native POST body leaked into GET upgrade")
			}
			return http.DefaultTransport.RoundTrip(upgrade)
		})
		// Production bridge selection must retain this native selector rather
		// than choosing Provider.RoundTrip and bypassing the selected fallback.
		transport := gateway.OverrideProviderTransport(bridge.Provider{Chat: Provider{}}, req, fallback)
		response, err := transport.RoundTrip(out)
		if err != nil {
			t.Fatal(err)
		}
		s := (Provider{}).Decode(req, response)
		var text string
		var usage *gateway.Usage
		for range 8 {
			event, err := s.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if event.Usage != nil {
				usage = event.Usage
			}
			if event.Kind == gateway.EventData {
				field := "choices.0.message.content"
				if stream {
					field = "choices.0.delta.content"
				}
				text += gjson.GetBytes(event.Payload, field).Str
			}
		}
		_ = s.Close()
		if text != "native reply" || usage == nil || usage.PromptTokens != 7 || usage.CompletionTokens != 2 || calls.Load() != 1 {
			t.Fatalf("selected exchange changed: text=%q usage=%+v calls=%d", text, usage, calls.Load())
		}
	}
}

func TestSelectedNativeTransportPolicyErrorsAndHandshakeFailuresNeverBypassOrLeak(t *testing.T) {
	var originCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		originCalls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	for _, mode := range []string{"policy", "reflected_error", "missing_fallback", "non101", "nonduplex", "bad_accept"} {
		t.Run(mode, func(t *testing.T) {
			req, out := transportRequest(t, context.Background(), server.URL, true)
			before := originCalls.Load()
			var calls int
			var fallback http.RoundTripper = selectedRoundTripper(func(upgrade *http.Request) (*http.Response, error) {
				calls++
				switch mode {
				case "policy":
					return nil, httpx.ErrMarketTransportPolicy
				case "reflected_error":
					return nil, errors.New(upgrade.URL.String() + " test-secret test-key")
				case "nonduplex", "bad_accept":
					body := io.NopCloser(strings.NewReader(""))
					if mode == "bad_accept" {
						body = &testDuplexBody{Reader: strings.NewReader("")}
					}
					return &http.Response{StatusCode: 101, Header: http.Header{"Upgrade": {"websocket"}, "Connection": {"Upgrade"}, "Sec-Websocket-Accept": {"invalid"}}, Body: body}, nil
				}
				return http.DefaultTransport.RoundTrip(upgrade)
			})
			if mode == "missing_fallback" {
				fallback = nil
			}
			_, err := (Provider{}).UpstreamTransport(req, fallback).RoundTrip(out)
			if err == nil || errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "authorization") || strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), "test-key") || strings.Contains(err.Error(), server.URL) {
				t.Fatalf("selected rejection missing or leaked signed URL/credentials: %v", err)
			}
			wantCalls := 1
			if mode == "missing_fallback" {
				wantCalls = 0
			}
			if calls != wantCalls || (mode != "non101" && originCalls.Load() != before) {
				t.Fatal("selected fallback rejected but native transport dialed independently")
			}
		})
	}
}

type testDuplexBody struct{ *strings.Reader }

func (*testDuplexBody) Write(data []byte) (int, error) { return len(data), nil }
func (*testDuplexBody) Close() error                   { return nil }

func TestSelectedSocketCancellationUnblocksNativeRead(t *testing.T) {
	disconnected := make(chan struct{})
	server := nativeServer(t, func(conn *websocket.Conn) {
		defer close(disconnected)
		var body []byte
		if websocket.Message.Receive(conn, &body) != nil {
			return
		}
		_ = websocket.Message.Send(conn, nativeFrame(0, 0, "partial", ""))
		_ = websocket.Message.Receive(conn, &body)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, out := transportRequest(t, ctx, server.URL, true)
	response, err := (Provider{}).UpstreamTransport(req, http.DefaultTransport).RoundTrip(out)
	if err != nil {
		t.Fatal(err)
	}
	s := (Provider{}).Decode(req, response)
	defer func() { _ = s.Close() }()
	if event, err := s.Next(); err != nil || event.TextBytes != 7 {
		t.Fatalf("selected socket closed immediately after handshake: %+v %v", event, err)
	}
	result := make(chan error, 1)
	go func() { _, err := s.Next(); result <- err }()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("selected read lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("selected read ignored cancellation")
	}
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("selected socket leaked after cancellation")
	}
}

func TestSelectedHandshakeHonorsRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, out := transportRequest(t, ctx, "https://8.8.8.8", true)
	fallback := selectedRoundTripper(func(upgrade *http.Request) (*http.Response, error) {
		<-upgrade.Context().Done()
		return nil, upgrade.Context().Err()
	})
	_, err := (Provider{}).UpstreamTransport(req, fallback).RoundTrip(out)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("selected handshake lost parent deadline: %v", err)
	}
}
