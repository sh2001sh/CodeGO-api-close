package credentials

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

func TestCredentialImageFactoryWorksThroughRealHTTPAndHTTPSProxy(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "https"}[secure], func(t *testing.T) {
			payload := []byte("\x89PNG\r\n\x1a\nimage fixture")
			origin := publicIPTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host != "8.8.8.8" || r.UserAgent() != "image-credential" {
					t.Errorf("image origin identity=%s %v", r.Host, r.Header)
				}
				for _, header := range []string{"Authorization", "Cookie", "Proxy-Authorization", "X-Api-Key", "X-Goog-Api-Key"} {
					if r.Header.Get(header) != "" {
						t.Errorf("origin credentials leaked: %s", header)
					}
				}
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write(payload)
			}))
			defer origin.Close()
			connects := atomic.Int32{}
			var tunnels sync.WaitGroup
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodConnect || r.Host != "8.8.8.8:443" {
					t.Errorf("proxy origin not pinned: %s %s", r.Method, r.Host)
					w.WriteHeader(400)
					return
				}
				if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-password")) || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Errorf("proxy auth isolation=%v", r.Header)
					w.WriteHeader(407)
					return
				}
				server, err := net.Dial("tcp", origin.Listener.Addr().String())
				if err != nil {
					t.Error(err)
					w.WriteHeader(502)
					return
				}
				client, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					_ = server.Close()
					t.Error(err)
					return
				}
				connects.Add(1)
				tunnels.Add(1)
				defer tunnels.Done()
				defer func() { _ = server.Close(); _ = client.Close() }()
				if _, err = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
					return
				}
				done := make(chan struct{})
				go func() { _, _ = io.Copy(server, client); _ = server.Close(); close(done) }()
				_, _ = io.Copy(client, server)
				_ = client.Close()
				<-done
			})
			var proxy *httptest.Server
			if secure {
				proxy = httptest.NewTLSServer(handler)
			} else {
				proxy = httptest.NewServer(handler)
			}
			defer proxy.Close()
			roots := x509.NewCertPool()
			roots.AddCert(origin.Certificate())
			if secure {
				roots.AddCert(proxy.Certificate())
			}
			proxyURL, err := url.Parse(proxy.URL)
			if err != nil {
				t.Fatal(err)
			}
			proxyURL.User = url.UserPassword("proxy-user", "proxy-password")
			pool := NewTransportPool(TransportConfig{RootCAs: roots})
			defer pool.CloseIdle()
			client, fp, err := pool.Client(1, proxyURL.String(), Fingerprint{TLSProfile: "firefox", UserAgent: "image-credential"})
			if err != nil {
				t.Fatal(err)
			}
			if fp.TLSProfile != "firefox" {
				t.Fatal("credential fingerprint lost")
			}
			if _, ok := client.Transport.(httpx.PinnedImageTransport); !ok {
				t.Fatal("pool transport lacks image factory")
			}
			image, err := httpx.FetchImage(context.Background(), "https://8.8.8.8/image", httpx.ImageFetchConfig{Transport: client.Transport})
			if err != nil {
				t.Fatal(err)
			}
			tunnels.Wait()
			if image.MIMEType != "image/png" || string(image.Data) != string(payload) || connects.Load() != 1 {
				t.Fatalf("image=%s %q connects=%d", image.MIMEType, image.Data, connects.Load())
			}
		})
	}
}

func TestImageProxyAndOriginTLSVerificationCannotBeDisabled(t *testing.T) {
	origin := publicIPTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "image") }))
	defer origin.Close()
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("untrusted HTTPS proxy reached CONNECT handler") }))
	defer proxy.Close()
	roots := x509.NewCertPool()
	roots.AddCert(origin.Certificate())
	pool := NewTransportPool(TransportConfig{RootCAs: roots})
	defer pool.CloseIdle()
	client, _, err := pool.Client(1, proxy.URL, Fingerprint{})
	if err != nil {
		t.Fatal(err)
	}
	factory := client.Transport.(httpx.PinnedImageTransport)
	transport, err := factory.PublicImageTransport("8.8.8.8", netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	defer transport.(interface{ CloseIdleConnections() }).CloseIdleConnections()
	resp, err := (&http.Client{Transport: transport}).Get("https://8.8.8.8/image")
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("untrusted proxy certificate accepted")
	}
}
