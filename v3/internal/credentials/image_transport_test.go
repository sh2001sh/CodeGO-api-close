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

func TestPinnedImagePreservesTLSFingerprintHostnameAndRootVerification(t *testing.T) {
	for _, profile := range []string{"chrome", "firefox"} {
		t.Run(profile, func(t *testing.T) {
			var curve atomic.Uint32
			origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host != "example.com" || r.TLS.ServerName != "example.com" || r.UserAgent() != "image-credential" || r.Header.Get("Accept") != "image/*" {
					t.Errorf("origin identity=%s %s %v", r.Host, r.TLS.ServerName, r.Header)
				}
				for _, header := range []string{"Authorization", "Cookie", "Proxy-Authorization", "X-Api-Key", "X-Goog-Api-Key", "X-Credential-Secret", "Anthropic-Version"} {
					if r.Header.Get(header) != "" {
						t.Errorf("provider header %s leaked", header)
					}
				}
				_, _ = io.WriteString(w, "image")
			}))
			origin.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
				if len(hello.SupportedCurves) > 0 {
					curve.Store(uint32(hello.SupportedCurves[0]))
				}
				if len(hello.SupportedProtos) != 1 || hello.SupportedProtos[0] != "http/1.1" {
					t.Errorf("unexpected ALPN=%v", hello.SupportedProtos)
				}
				return nil, nil
			}}
			origin.StartTLS()
			defer origin.Close()
			roots := x509.NewCertPool()
			roots.AddCert(origin.Certificate())
			dials := atomic.Int32{}
			dial := func(ctx context.Context, network, address string) (net.Conn, error) {
				dials.Add(1)
				if address != "8.8.8.8:443" {
					t.Errorf("unpinned origin=%s", address)
				}
				return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
			}
			transport, err := newImageTransport("example.com", netip.MustParseAddr("8.8.8.8"), TransportConfig{RootCAs: roots}, Fingerprint{TLSProfile: profile, UserAgent: "image-credential"}, dial)
			if err != nil {
				t.Fatal(err)
			}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport}
			for range 2 {
				req, err := http.NewRequest(http.MethodGet, "https://example.com/image", nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Accept", "image/*")
				req.Header.Set("User-Agent", "caller-ua")
				for _, header := range []string{"Authorization", "Cookie", "Proxy-Authorization", "X-Api-Key", "X-Goog-Api-Key", "X-Credential-Secret", "Anthropic-Version"} {
					req.Header.Set(header, "must-not-forward")
				}
				req.Host = "provider.example"
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil || string(body) != "image" {
					t.Fatalf("body=%q error=%v", body, err)
				}
				if req.Header.Get("Authorization") != "must-not-forward" || req.Header.Get("User-Agent") != "caller-ua" || req.Host != "provider.example" {
					t.Fatal("original request mutated")
				}
			}
			if dials.Load() != 1 {
				t.Fatalf("image connection not reused: dials=%d", dials.Load())
			}
			first := curve.Load()
			if profile == "firefox" && first != uint32(tls.X25519) {
				t.Fatalf("Firefox hello replaced: first curve=%x", first)
			}
			if profile == "chrome" && (first&0x0f0f != 0x0a0a || first>>8 != first&0xff) {
				t.Fatalf("Chrome GREASE hello replaced: first curve=%x", first)
			}
		})
	}
}

func TestPinnedImageRejectsUntrustedOrWrongHostnameCertificates(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
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
		transport, err := newImageTransport(tc.host, netip.MustParseAddr("8.8.8.8"), TransportConfig{RootCAs: tc.roots}, Fingerprint{}, dial)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := (&http.Client{Transport: transport}).Get("https://" + tc.host + "/image")
		if resp != nil {
			_ = resp.Body.Close()
		}
		transport.CloseIdleConnections()
		if err == nil {
			t.Fatalf("unsafe certificate accepted for %s", tc.host)
		}
	}
}

func TestImageFactoryRejectsNonpublicAddressesAndUnsafeHosts(t *testing.T) {
	dials := atomic.Int32{}
	dial := func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("should not dial")
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "198.18.0.1", "203.0.113.1", "::1", "fe80::1", "fd00::1", "2001:db8::1", "::ffff:127.0.0.1", "64:ff9b::7f00:1"} {
		if _, err := newImageTransport("example.com", netip.MustParseAddr(raw), TransportConfig{}, Fingerprint{}, dial); !errors.Is(err, httpx.ErrImageTransportPolicy) {
			t.Fatalf("unsafe address %s accepted: %v", raw, err)
		}
	}
	for _, host := range []string{"", "user@example.com", "example.com:443", "example.com/path", "example.com\r\nInjected: yes", "fe80::1%eth0"} {
		if _, err := newImageTransport(host, netip.MustParseAddr("8.8.8.8"), TransportConfig{}, Fingerprint{}, dial); !errors.Is(err, httpx.ErrImageTransportPolicy) {
			t.Fatalf("unsafe host %q accepted: %v", host, err)
		}
	}
	if dials.Load() != 0 {
		t.Fatal("rejected factory dialed network")
	}
	var source *identityTransport
	if _, err := source.PublicImageTransport("example.com", netip.MustParseAddr("8.8.8.8")); !errors.Is(err, httpx.ErrImageTransportPolicy) {
		t.Fatalf("nil source accepted: %v", err)
	}
	var empty *imageTransport
	empty.CloseIdleConnections()
	(&imageTransport{}).CloseIdleConnections()
}

func TestImageTransportCannotBeReusedForAnotherHostBodyOrProtocol(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		t.Error("invalid image request reached network")
		return nil, errors.New("unexpected dial")
	}
	transport, err := newImageTransport("example.com", netip.MustParseAddr("8.8.8.8"), TransportConfig{}, Fingerprint{}, dial)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	for _, tc := range []struct{ method, address, body string }{
		{"GET", "https://provider.example/image", ""}, {"POST", "https://example.com/image", "secret"},
		{"GET", "https://example.com/image", "secret"}, {"GET", "ftp://example.com/image", ""},
		{"GET", "https://user:pass@example.com/image", ""}, {"GET", "https://example.com/image#fragment", ""},
	} {
		var body io.Reader
		if tc.body != "" {
			body = strings.NewReader(tc.body)
		}
		req, err := http.NewRequest(tc.method, tc.address, body)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = transport.RoundTrip(req); !errors.Is(err, httpx.ErrImageTransportPolicy) {
			t.Fatalf("unsafe request accepted: %s %s %v", tc.method, tc.address, err)
		}
	}
}
