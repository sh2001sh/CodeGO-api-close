package httpx

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var imagePNG = []byte("\x89PNG\r\n\x1a\nimage payload")

func imageLookup(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
}

func imageTestTransport(t *testing.T, handler http.HandlerFunc) (*http.Transport, *atomic.Int32) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	dials := &atomic.Int32{}
	return &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		if address != "8.8.8.8:80" {
			t.Errorf("DNS result was not pinned: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}, dials
}

func TestFetchImagePinsPublicDNSAndNeverForwardsOriginAuth(t *testing.T) {
	transport, dials := imageTestTransport(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "image.example" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Goog-Api-Key") != "" {
			t.Errorf("invalid public-image request host/headers: %s %v", r.Host, r.Header)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imagePNG)
	})
	lookups := 0
	lookup := func(ctx context.Context, network, host string) ([]netip.Addr, error) {
		lookups++
		if lookups > 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return imageLookup(ctx, network, host)
	}
	image, err := fetchImage(context.Background(), "http://image.example/image?signature=test", ImageFetchConfig{Transport: transport}, lookup)
	if err != nil || image.MIMEType != "image/png" || string(image.Data) != string(imagePNG) || lookups != 1 || dials.Load() != 1 {
		t.Fatalf("image/DNS pinning failed: %+v %v lookups=%d dials=%d", image, err, lookups, dials.Load())
	}
}

func TestFetchImageRejectsUnsafeURLsAndMixedPrivateDNS(t *testing.T) {
	for _, raw := range []string{"file:///secret", "ftp://image.example/image", "https://user:secret@image.example/image", "http://127.0.0.1/image", "http://[::1]/image", "https://image.example/image#fragment"} {
		if _, err := FetchImage(context.Background(), raw, ImageFetchConfig{}); err == nil {
			t.Errorf("unsafe URL accepted: %s", raw)
		}
	}
	for _, address := range []string{"10.0.0.1", "169.254.169.254", "::ffff:127.0.0.1", "100.64.1.1", "192.0.2.1", "2001:db8::1", "64:ff9b::a00:1", "2002:a00:1::"} {
		lookup := func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr(address)}, nil
		}
		if _, err := fetchImage(context.Background(), "http://image.example/image", ImageFetchConfig{}, lookup); err == nil {
			t.Errorf("mixed private DNS accepted: %s", address)
		}
	}
}

func TestFetchImageRejectsRedirectOversizeAndNonimage(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://127.0.0.1/secret", http.StatusFound)
		},
		"oversize header":   func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Content-Length", "90000") },
		"oversize streamed": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 2048))) },
		"not an image":      func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>secret</html>")) },
		"mismatched type": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(imagePNG)
		},
	} {
		t.Run(name, func(t *testing.T) {
			transport, dials := imageTestTransport(t, handler)
			_, err := fetchImage(context.Background(), "http://image.example/image", ImageFetchConfig{Transport: transport, MaxBytes: 1024}, imageLookup)
			if err == nil || dials.Load() != 1 || strings.Contains(err.Error(), "127.0.0.1") {
				t.Fatalf("invalid source was accepted/redirected/leaked: %v dials=%d", err, dials.Load())
			}
		})
	}
}

func TestFetchImageTimeoutAndCanceledContext(t *testing.T) {
	transport, _ := imageTestTransport(t, func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	if _, err := fetchImage(context.Background(), "http://image.example/image", ImageFetchConfig{Transport: transport, Timeout: 20 * time.Millisecond}, imageLookup); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("fetch timeout lost: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FetchImage(ctx, "http://image.example/image", ImageFetchConfig{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation lost: %v", err)
	}
}

func TestFetchImagePreservesProxyWhilePinningOrigin(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "8.8.8.8:80" {
			t.Errorf("proxy would resolve unpinned origin: %s %s", r.Method, r.Host)
			w.WriteHeader(400)
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		origin, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil || origin.Host != "image.example" {
			t.Errorf("origin virtual host was lost: %+v %v", origin, err)
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nConnection: close\r\nContent-Length: 21\r\n\r\n")
		_, _ = conn.Write(imagePNG)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	if _, err := fetchImage(context.Background(), "http://image.example/image", ImageFetchConfig{Transport: transport}, imageLookup); err != nil {
		t.Fatal(err)
	}
}

type unsupportedImageTransport struct{}

func (unsupportedImageTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unexpected network request")
}

func TestFetchImageDoesNotSilentlyDiscardCustomTransport(t *testing.T) {
	_, err := fetchImage(context.Background(), "http://image.example/image", ImageFetchConfig{Transport: unsupportedImageTransport{}}, imageLookup)
	if err == nil || !strings.Contains(err.Error(), "pinning") {
		t.Fatalf("custom TLS/proxy silently ignored: %v", err)
	}
}

func TestFetchImagePinsHTTPSWhilePreservingOriginCertificateVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS.ServerName != "example.com" || r.Host != "example.com" {
			t.Errorf("original TLS/HTTP identity was lost: SNI=%s Host=%s", r.TLS.ServerName, r.Host)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imagePNG)
	}))
	defer server.Close()
	certificate, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "8.8.8.8:443" {
				t.Errorf("HTTPS destination was not pinned: %s", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		}}
	if _, err := fetchImage(context.Background(), "https://example.com/image", ImageFetchConfig{Transport: transport}, imageLookup); err != nil {
		t.Fatalf("pinned HTTPS certificate verification failed: %v", err)
	}
	transport.TLSClientConfig.InsecureSkipVerify = true
	if _, err := fetchImage(context.Background(), "https://example.com/image", ImageFetchConfig{Transport: transport}, imageLookup); !errors.Is(err, ErrImageTransportPolicy) {
		t.Fatalf("insecure image TLS was accepted: %v", err)
	}
}

type pinnedImageFactory struct {
	host    string
	address netip.Addr
	target  string
}

func (*pinnedImageFactory) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unprepared image transport must not be used")
}

func (p *pinnedImageFactory) PublicImageTransport(host string, address netip.Addr) (http.RoundTripper, error) {
	p.host, p.address = host, address
	return &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, p.target)
	}}, nil
}

func TestFetchImageUsesCustomTLSFactoryWithValidatedAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imagePNG)
	}))
	defer server.Close()
	factory := &pinnedImageFactory{target: server.Listener.Addr().String()}
	if _, err := fetchImage(context.Background(), "http://image.example/image", ImageFetchConfig{Transport: factory}, imageLookup); err != nil {
		t.Fatal(err)
	}
	if factory.host != "image.example" || factory.address != netip.MustParseAddr("8.8.8.8") {
		t.Fatalf("custom TLS did not receive validated/pinned origin: %+v", factory)
	}
}
