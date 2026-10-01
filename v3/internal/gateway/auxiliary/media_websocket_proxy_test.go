package auxiliary

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestVolcMediaWebsocketUsesAuthenticatedHTTPProxy(t *testing.T) {
	server := httptest.NewTLSServer(websocket.Handler(func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		if conn.Request().Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy password exposed to speech provider")
		}
		var frame []byte
		_ = websocket.Message.Receive(conn, &frame)
		_ = websocket.Message.Send(conn, volcMediaAudioFrame(-1, []byte{0, 1, 2, 255}))
	}))
	defer server.Close()
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("proxy method %s", r.Method)
			w.WriteHeader(400)
			return
		}
		if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-password")) {
			t.Error("missing proxy authentication")
			w.WriteHeader(407)
			return
		}
		remote, err := net.DialTimeout("tcp", r.Host, time.Second)
		if err != nil {
			t.Error(err)
			w.WriteHeader(502)
			return
		}
		defer func() { _ = remote.Close() }()
		client, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = client.Close() }()
		_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffer.Flush()
		calls.Add(1)
		go func() { _, _ = io.Copy(remote, client); _ = remote.Close() }()
		_, _ = io.Copy(client, remote)
	}))
	defer proxy.Close()
	p, _ := url.Parse(proxy.URL)
	p.User = url.UserPassword("proxy-user", "proxy-password")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r, _ := localVolcMediaRequest(t, ctx, server.URL)
	r.URL.Scheme = "wss"
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(p)
	defer transport.CloseIdleConnections()
	selected := &http.Client{Transport: transport}
	resp, err := mediaWebsocketRoundTrip(context.WithValue(ctx, clientKey{}, selected), r)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if calls.Load() != 1 {
		t.Fatalf("proxy connections %d", calls.Load())
	}
}

func TestVolcMediaWebsocketRejectsProxyFailure(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(407) }))
	defer proxy.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, _ := localVolcMediaRequest(t, ctx, "http://127.0.0.1:12345")
	proxyURL, _ := url.Parse(proxy.URL)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	defer transport.CloseIdleConnections()
	_, err := mediaWebsocketRoundTrip(context.WithValue(ctx, clientKey{}, &http.Client{Transport: transport}), r)
	if err == nil {
		t.Fatal("proxy authentication rejection ignored")
	}
}
