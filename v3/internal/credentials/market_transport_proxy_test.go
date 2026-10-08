package credentials

import (
	"crypto/x509"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

func TestProductionMarketClientPreservesNativePOSTThroughCredentialHTTPAndHTTPSProxies(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "https"}[secure], func(t *testing.T) {
			var origins atomic.Int32
			origin := publicIPTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				origins.Add(1)
				body, _ := io.ReadAll(r.Body)
				if r.Method != http.MethodPost || string(body) != `{"input":"native payload"}` || r.Header.Get("Authorization") != "Bearer native-key" || r.Header.Get("X-Api-Key") != "native-key" || r.UserAgent() != "market-credential" || r.Host != "8.8.8.8" {
					t.Errorf("native request identity/body lost: %s %s %v %q", r.Method, r.Host, r.Header, body)
				}
				if r.Header.Get("Proxy-Authorization") != "" {
					t.Error("proxy authentication reached origin")
				}
				w.Header().Set("Location", "https://127.0.0.1/private")
				w.WriteHeader(http.StatusTemporaryRedirect)
				_, _ = io.WriteString(w, "native response")
			}))
			defer origin.Close()
			proxy, connects := marketProxyFixture(t, origin.Listener.Addr().String(), secure)
			roots := x509.NewCertPool()
			roots.AddCert(origin.Certificate())
			if secure {
				roots.AddCert(proxy.Certificate())
			}
			proxyURL, _ := url.Parse(proxy.URL)
			proxyURL.User = url.UserPassword("proxy-user", "proxy-password")
			pool := NewTransportPool(TransportConfig{RootCAs: roots})
			defer pool.CloseIdle()
			selected, _, err := pool.Client(1, proxyURL.String(), Fingerprint{TLSProfile: "chrome", UserAgent: "market-credential"})
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := selected.Transport.(httpx.PinnedMarketTransport); !ok {
				t.Fatal("production credential transport lacks market factory")
			}
			market, err := httpx.MarketClient(selected)
			if err != nil {
				t.Fatal(err)
			}
			defer market.CloseIdleConnections()
			for i := int32(1); i <= 3; i++ {
				req, _ := http.NewRequest(http.MethodPost, "https://8.8.8.8/v1/responses", strings.NewReader(`{"input":"native payload"}`))
				req.Header.Set("Authorization", "Bearer native-key")
				req.Header.Set("X-Api-Key", "native-key")
				response, err := market.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if err != nil || string(body) != "native response" || response.StatusCode != http.StatusTemporaryRedirect || origins.Load() != i || connects.Load() != 1 || market.Transport == selected.Transport {
					t.Fatalf("native market pooling/redirect isolation lost: body=%q origins=%d connects=%d error=%v", body, origins.Load(), connects.Load(), err)
				}
			}
		})
	}
}

func marketProxyFixture(t *testing.T, origin string, secure bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var tunnels sync.WaitGroup
	connects := &atomic.Int32{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "8.8.8.8:443" || r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-password")) || r.Header.Get("Authorization") != "" || r.Header.Get("X-Api-Key") != "" {
			t.Errorf("CONNECT address/auth isolation lost: %s %s %v", r.Method, r.Host, r.Header)
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		upstream, err := net.Dial("tcp", origin)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadGateway)
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
		if _, err = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		done := make(chan struct{})
		go func() { _, _ = io.Copy(upstream, client); _ = upstream.Close(); close(done) }()
		_, _ = io.Copy(client, upstream)
		_ = client.Close()
		<-done
	})
	var proxy *httptest.Server
	if secure {
		proxy = httptest.NewTLSServer(handler)
	} else {
		proxy = httptest.NewServer(handler)
	}
	t.Cleanup(func() { proxy.Close(); tunnels.Wait() })
	return proxy, connects
}

func TestMarketProductionClientRejectsUntrustedProxyCertificate(t *testing.T) {
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("untrusted proxy received origin credentials") }))
	defer proxy.Close()
	pool := NewTransportPool(TransportConfig{})
	defer pool.CloseIdle()
	selected, _, err := pool.Client(1, proxy.URL, Fingerprint{})
	if err != nil {
		t.Fatal(err)
	}
	market, err := httpx.MarketClient(selected)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, "https://8.8.8.8/v1/messages", strings.NewReader("private prompt"))
	req.Header.Set("Authorization", "Bearer native-key")
	response, err := market.Do(req)
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("untrusted HTTPS proxy accepted")
	}
}
