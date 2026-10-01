package credentials

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

func TestMarketPinnedTransportPreservesNativePOSTAndTLSIdentity(t *testing.T) {
	const payload = `{"model":"native-model","messages":[{"role":"user","content":"你好"}],"stream":true}`
	var firstCurve atomic.Uint32
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || string(body) != payload || r.ContentLength != int64(len(payload)) {
			t.Errorf("native request changed: method=%s bytes=%q length=%d", r.Method, body, r.ContentLength)
		}
		if r.Host != "example.com" || r.TLS.ServerName != "example.com" || r.UserAgent() != "native-credential" {
			t.Errorf("native identity changed: host=%s sni=%s ua=%s", r.Host, r.TLS.ServerName, r.UserAgent())
		}
		for header, want := range map[string]string{"Authorization": "Bearer upstream-native-key", "X-Api-Key": "native-api-key", "Cookie": "provider-session=fixture", "Anthropic-Version": "2023-06-01", "Content-Type": "application/json"} {
			if r.Header.Get(header) != want {
				t.Errorf("native header %s lost", header)
			}
		}
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials reached origin")
		}
		_, _ = io.WriteString(w, `{"native":"response"}`)
	}))
	origin.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		if len(hello.SupportedCurves) > 0 {
			firstCurve.Store(uint32(hello.SupportedCurves[0]))
		}
		return nil, nil
	}}
	origin.StartTLS()
	defer origin.Close()
	roots := x509.NewCertPool()
	roots.AddCert(origin.Certificate())
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "8.8.8.8:443" {
			t.Errorf("native origin not pinned: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
	}
	transport, err := newMarketTransport("example.com", netip.MustParseAddr("8.8.8.8"), TransportConfig{RootCAs: roots}, Fingerprint{TLSProfile: "firefox", UserAgent: "native-credential"}, dial)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	req, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", strings.NewReader(payload))
	req.Header = http.Header{"Authorization": {"Bearer upstream-native-key"}, "X-Api-Key": {"native-api-key"}, "Cookie": {"provider-session=fixture"}, "Anthropic-Version": {"2023-06-01"}, "Content-Type": {"application/json"}, "User-Agent": {"caller-ua"}, "Proxy-Authorization": {"must-not-forward"}}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || string(body) != `{"native":"response"}` || firstCurve.Load() != uint32(tls.X25519) {
		t.Fatalf("native TLS/response changed: response=%q curve=%x error=%v", body, firstCurve.Load(), err)
	}
	if req.Header.Get("User-Agent") != "caller-ua" || req.Header.Get("Proxy-Authorization") != "must-not-forward" || req.GetBody == nil || req.ContentLength != int64(len(payload)) {
		t.Fatal("caller request mutated")
	}
	replay, err := req.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = replay.Close() }()
	replayed, err := io.ReadAll(replay)
	if err != nil || string(replayed) != payload {
		t.Fatal("native replay body lost")
	}
}

func TestMarketFactoryRejectsPrivateAddressUnsafeRequestsAndHostOverrides(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		t.Error("unsafe market request reached network")
		return nil, errors.New("unexpected dial")
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "2001:db8::1", "::ffff:127.0.0.1"} {
		if _, err := newMarketTransport("example.com", netip.MustParseAddr(raw), TransportConfig{}, Fingerprint{}, dial); !errors.Is(err, httpx.ErrMarketTransportPolicy) {
			t.Fatalf("private market factory accepted %s: %v", raw, err)
		}
	}
	for _, host := range []string{"", "example.com:443", "user@example.com", "example.com/path", "example.com\r\nInjected: yes"} {
		if _, err := newMarketTransport(host, netip.MustParseAddr("8.8.8.8"), TransportConfig{}, Fingerprint{}, dial); !errors.Is(err, httpx.ErrMarketTransportPolicy) {
			t.Fatalf("unsafe factory host accepted %q: %v", host, err)
		}
	}
	transport, err := newMarketTransport("example.com", netip.MustParseAddr("8.8.8.8"), TransportConfig{}, Fingerprint{}, dial)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	for _, tc := range []struct{ url, host string }{
		{"http://example.com/v1/messages", ""}, {"https://other.example/v1/messages", ""},
		{"https://example.com:8080/v1/messages", ""}, {"https://user:password@example.com/v1/messages", ""},
		{"https://example.com/v1/messages#fragment", ""}, {"https://example.com/v1/messages", "127.0.0.1"},
		{"https://example.com/v1/messages", "private.example"}, {"https://example.com/v1/messages", "example.com:8443"},
	} {
		req, _ := http.NewRequest(http.MethodPost, tc.url, strings.NewReader("private prompt"))
		req.Host = tc.host
		if _, err := transport.RoundTrip(req); !errors.Is(err, httpx.ErrMarketTransportPolicy) {
			t.Fatalf("unsafe market request accepted %s host=%s: %v", tc.url, tc.host, err)
		}
	}
	var nilSource *identityTransport
	if _, err := nilSource.PublicMarketTransport("example.com", netip.MustParseAddr("8.8.8.8")); !errors.Is(err, httpx.ErrMarketTransportPolicy) {
		t.Fatalf("nil factory accepted: %v", err)
	}
	var nilTransport *marketRelayTransport
	nilTransport.CloseIdleConnections()
	(&marketRelayTransport{}).CloseIdleConnections()
	if _, err := nilTransport.RoundTrip(nil); !errors.Is(err, httpx.ErrMarketTransportPolicy) {
		t.Fatalf("nil transport accepted: %v", err)
	}
}

func TestMarketPinnedTransportCannotBypassOriginCertificateTrust(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid certificate reached origin") }))
	defer origin.Close()
	roots := x509.NewCertPool()
	roots.AddCert(origin.Certificate())
	for _, tc := range []struct {
		host  string
		roots *x509.CertPool
	}{{"example.com", nil}, {"wrong.example", roots}} {
		dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
		}
		transport, err := newMarketTransport(tc.host, netip.MustParseAddr("8.8.8.8"), TransportConfig{RootCAs: tc.roots}, Fingerprint{}, dial)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(http.MethodPost, "https://"+tc.host+"/v1/messages", strings.NewReader("private prompt"))
		_, err = transport.RoundTrip(req)
		transport.CloseIdleConnections()
		if err == nil {
			t.Fatalf("unsafe origin certificate accepted for %s", tc.host)
		}
	}
}
