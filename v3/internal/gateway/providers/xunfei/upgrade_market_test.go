package xunfei

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/credentials"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

func TestSelectedMarketSparkUsesActualCredentialProxyPinTLSFingerprintAndDuplexBody(t *testing.T) {
	var curves atomic.Uint32
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Host != "8.8.8.8" || r.URL.Query().Get("authorization") == "" || r.UserAgent() != "spark-market-credential" || r.Header.Get("X-Final-Header") != "retained" {
			t.Error("real selected market upgrade lost signing or credential identity")
		}
		websocket.Handler(func(conn *websocket.Conn) {
			defer func() { _ = conn.Close() }()
			var body []byte
			if err := websocket.Message.Receive(conn, &body); err != nil {
				t.Errorf("market body lost duplex Write: %v", err)
				return
			}
			if gjson.GetBytes(body, "header.app_id").Str != "market-app" || gjson.GetBytes(body, "payload.message.text.0.content").Str != "hello" {
				t.Errorf("native market frame changed: %s", body)
			}
			_ = websocket.Message.Send(conn, nativeFrame(2, 0, "safe market reply", `{"prompt_tokens":3,"completion_tokens":4}`))
		}).ServeHTTP(w, r)
	}))
	origin.TLS = &tls.Config{Certificates: []tls.Certificate{marketSparkCertificate(t)}, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		if len(hello.SupportedCurves) > 0 {
			curves.Store(uint32(hello.SupportedCurves[0]))
		}
		return nil, nil
	}}
	origin.StartTLS()
	defer origin.Close()
	var tunnels sync.WaitGroup
	var connects atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "8.8.8.8:443" {
			t.Errorf("market origin resolved again instead of pinned: %s %s", r.Method, r.Host)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
		if err != nil {
			t.Error(err)
			return
		}
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = upstream.Close()
			t.Error(err)
			return
		}
		connects.Add(1)
		tunnels.Add(1)
		defer tunnels.Done()
		defer func() { _ = upstream.Close(); _ = client.Close() }()
		_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		done := make(chan struct{})
		go func() { _, _ = io.Copy(upstream, client); _ = upstream.Close(); close(done) }()
		_, _ = io.Copy(client, upstream)
		_ = client.Close()
		<-done
	}))
	defer func() { proxy.Close(); tunnels.Wait() }()
	roots := x509.NewCertPool()
	roots.AddCert(origin.Certificate())
	pool := credentials.NewTransportPool(credentials.TransportConfig{RootCAs: roots})
	defer pool.CloseIdle()
	selected, _, err := pool.Client(1, proxy.URL, credentials.Fingerprint{TLSProfile: "firefox", UserAgent: "spark-market-credential"})
	if err != nil {
		t.Fatal(err)
	}
	market, err := httpx.MarketClient(selected)
	if err != nil {
		t.Fatal(err)
	}
	req := chatRequest(true, `{"messages":[{"role":"user","content":"hello"}]}`)
	out, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{Secret: "market-app|market-secret|market-key", BaseURL: "https://8.8.8.8/v3.5/chat", ProxyURL: proxy.URL})
	if err != nil {
		t.Fatalf("configured native proxy rejected before selected transport: %v", err)
	}
	out.Header.Set("X-Final-Header", "retained")
	response, err := (Provider{}).UpstreamTransport(req, market.Transport).RoundTrip(out)
	if err != nil {
		t.Fatal(err)
	}
	s := (Provider{}).Decode(req, response)
	defer func() { _ = s.Close() }()
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
			text += gjson.GetBytes(event.Payload, "choices.0.delta.content").Str
		}
	}
	if text != "safe market reply" || usage == nil || usage.PromptTokens != 3 || usage.CompletionTokens != 4 || curves.Load() != uint32(tls.X25519) || connects.Load() != 1 {
		t.Fatalf("production market native exchange lost response/usage/fingerprint: text=%s usage=%+v curve=%x connects=%d", text, usage, curves.Load(), connects.Load())
	}
}

func marketSparkCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("8.8.8.8")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestSelectedActualMarketPolicyRejectsPrivateSparkBeforeAnyDial(t *testing.T) {
	var calls atomic.Int32
	base := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		return nil, errors.New("must not dial")
	}}
	market, err := httpx.MarketClient(&http.Client{Transport: base})
	if err != nil {
		t.Fatal(err)
	}
	req, out := transportRequest(t, context.Background(), "https://127.0.0.1/v3.5/chat", true)
	_, err = (Provider{}).UpstreamTransport(req, market.Transport).RoundTrip(out)
	if err == nil || calls.Load() != 0 || strings.Contains(err.Error(), "authorization") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("market native policy bypass/leak: calls=%d error=%v", calls.Load(), err)
	}
}
