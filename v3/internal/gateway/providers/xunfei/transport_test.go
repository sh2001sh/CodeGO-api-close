package xunfei

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

func nativeServer(t *testing.T, exchange func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(websocket.Handler(func(conn *websocket.Conn) { defer func() { _ = conn.Close() }(); exchange(conn) }))
	t.Cleanup(server.Close)
	return server
}

func transportRequest(t *testing.T, ctx context.Context, server string, stream bool) (*gateway.Request, *http.Request) {
	t.Helper()
	req := chatRequest(stream, `{"messages":[{"role":"user","content":"hello"}]}`)
	httpReq, err := (Provider{}).BuildRequest(ctx, req, gateway.Target{Secret: "test-app|test-secret|test-key", BaseURL: server})
	if err != nil {
		t.Fatal(err)
	}
	return req, httpReq
}

func TestNativeWebSocketExchangeAndDecode(t *testing.T) {
	captured := make(chan []byte, 1)
	server := nativeServer(t, func(conn *websocket.Conn) {
		var body []byte
		if websocket.Message.Receive(conn, &body) != nil {
			return
		}
		captured <- body
		_ = websocket.Message.Send(conn, nativeFrame(0, 0, "hello ", ""))
		_ = websocket.Message.Send(conn, nativeFrame(2, 1, "world", `{"prompt_tokens":7,"completion_tokens":2}`))
	})
	for _, stream := range []bool{true, false} {
		req, httpReq := transportRequest(t, context.Background(), server.URL, stream)
		resp, err := (Provider{}).RoundTrip(httpReq)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("bad native transport response")
		}
		body := <-captured
		if gjson.GetBytes(body, "header.app_id").Str != "test-app" || gjson.GetBytes(body, "payload.message.text.0.content").Str != "hello" {
			t.Fatalf("native request not sent: %s", body)
		}
		s := (Provider{}).Decode(req, resp)
		text := ""
		var usage *gateway.Usage
		done := false
		for i := 0; i < 8; i++ {
			ev, err := s.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if ev.Usage != nil {
				usage = ev.Usage
			}
			if ev.Kind == gateway.EventData {
				field := "choices.0.delta.content"
				if !stream {
					field = "choices.0.message.content"
				}
				text += gjson.GetBytes(ev.Payload, field).Str
			}
			if ev.Kind == gateway.EventDone {
				done = true
			}
		}
		_ = s.Close()
		if text != "hello world" || usage == nil || usage.PromptTokens != 7 || usage.CompletionTokens != 2 || (stream && !done) {
			t.Fatalf("exchange lost text/usage/finish: %q %+v %v", text, usage, done)
		}
	}
}

func TestTransportCancellationClosesSocketAndUnblocksRead(t *testing.T) {
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
	req, httpReq := transportRequest(t, ctx, server.URL, true)
	resp, err := (Provider{}).RoundTrip(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	s := (Provider{}).Decode(req, resp)
	defer func() { _ = s.Close() }()
	first, err := s.Next()
	if err != nil || first.TextBytes != 7 {
		t.Fatalf("missing partial: %+v %v", first, err)
	}
	result := make(chan error, 1)
	go func() { _, err := s.Next(); result <- err }()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel lost: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation blocked native reader")
	}
	select {
	case <-disconnected:
	case <-time.After(2 * time.Second):
		t.Fatal("socket leaked after cancellation")
	}
}

func TestTransportRequestDeadlineRemainsActiveAfterHandshake(t *testing.T) {
	server := nativeServer(t, func(conn *websocket.Conn) {
		var body []byte
		_ = websocket.Message.Receive(conn, &body)
		_ = websocket.Message.Receive(conn, &body)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req, httpReq := transportRequest(t, ctx, server.URL, true)
	resp, err := (Provider{}).RoundTrip(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	s := (Provider{}).Decode(req, resp)
	defer func() { _ = s.Close() }()
	if _, err := s.Next(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout lost: %v", err)
	}
}

func TestTransportHandshakeFailureNeverLeaksSignedURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	_, req := transportRequest(t, context.Background(), server.URL, true)
	_, err := (Provider{}).RoundTrip(req)
	if err == nil || strings.Contains(err.Error(), "authorization") || strings.Contains(err.Error(), "test-key") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("handshake failure leaked signed endpoint: %v", err)
	}
}

func TestTransportRejectsMalformedOversizedAndTruncatedFrames(t *testing.T) {
	for name, frame := range map[string]string{"malformed": "not JSON", "oversized": `{"text":"` + strings.Repeat("a", maxFrame) + `"}`, "truncated": nativeFrame(0, 0, "partial", "")} {
		t.Run(name, func(t *testing.T) {
			server := nativeServer(t, func(conn *websocket.Conn) {
				var body []byte
				if websocket.Message.Receive(conn, &body) == nil {
					_ = websocket.Message.Send(conn, frame)
				}
			})
			req, httpReq := transportRequest(t, context.Background(), server.URL, true)
			resp, err := (Provider{}).RoundTrip(httpReq)
			if err != nil {
				t.Fatal(err)
			}
			s := (Provider{}).Decode(req, resp)
			defer func() { _ = s.Close() }()
			for i := 0; i < 3; i++ {
				ev, err := s.Next()
				if err != nil {
					if errors.Is(err, io.EOF) {
						t.Fatal("broken frame ended cleanly")
					}
					return
				}
				if ev.Kind == gateway.EventDone {
					t.Fatal("broken frame emitted Done")
				}
			}
			t.Fatal("broken frame did not fail")
		})
	}
}
