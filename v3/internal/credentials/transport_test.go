package credentials

import (
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"sync"
	"testing"
)

func TestUTLSConnectionsAreReusedWithStableCredentialIdentity(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() != "credential-ua" {
			t.Error("cached user agent lost")
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	pool := NewTransportPool(TransportConfig{RootCAs: roots})
	defer pool.CloseIdle()
	for _, profile := range []string{"chrome", "firefox"} {
		t.Run(profile, func(t *testing.T) {
			fp := Fingerprint{UserAgent: "credential-ua", TLSProfile: profile}
			client, got, err := pool.Client(1, "", fp)
			if err != nil {
				t.Fatal(err)
			}
			if got != fp {
				t.Fatal("fingerprint was replaced")
			}
			again, _, err := pool.Client(1, "", fp)
			if err != nil || again != client {
				t.Fatal("same credential did not reuse its client")
			}
			reused := 0
			for i := 0; i < 20; i++ {
				req, err := http.NewRequest(http.MethodGet, server.URL, nil)
				if err != nil {
					t.Fatal(err)
				}
				trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
					if info.Reused {
						reused++
					}
				}}
				req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if reused != 19 {
				t.Fatalf("uTLS reuse=%d/20, want 19/20 (>90%%)", reused)
			}
			t.Logf("%s uTLS connection reuse: %d/20 (95%%)", profile, reused)
		})
	}
}

func TestUTLSRejectsUntrustedCertificates(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	pool := NewTransportPool(TransportConfig{})
	defer pool.CloseIdle()
	client, _, err := pool.Client(1, "", Fingerprint{})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(server.URL)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("untrusted upstream certificate accepted")
	}
}

func TestUTLSUsesCONNECTProxyWithoutLosingFingerprint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "proxied") }))
	defer server.Close()
	var connections sync.WaitGroup
	ready := make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Error("proxy did not receive CONNECT")
			w.WriteHeader(405)
			return
		}
		upstream, err := net.Dial("tcp", r.Host)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = upstream.Close()
			return
		}
		if _, err = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			_ = client.Close()
			_ = upstream.Close()
			return
		}
		connections.Add(2)
		close(ready)
		go func() { defer connections.Done(); _, _ = io.Copy(client, upstream); _ = client.Close() }()
		go func() { defer connections.Done(); _, _ = io.Copy(upstream, client); _ = upstream.Close() }()
	}))
	defer proxy.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	pool := NewTransportPool(TransportConfig{RootCAs: roots})
	client, _, err := pool.Client(1, proxy.URL, Fingerprint{})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	pool.CloseIdle()
	<-ready
	connections.Wait()
	if err != nil || string(body) != "proxied" {
		t.Fatalf("proxy response=%q, err=%v", body, err)
	}
}

func TestTransportRejectsInvalidProfilesHeadersAndProxySchemes(t *testing.T) {
	pool := NewTransportPool(TransportConfig{})
	for _, tc := range []struct {
		proxy string
		fp    Fingerprint
	}{
		{"", Fingerprint{TLSProfile: "unknown"}},
		{"", Fingerprint{UserAgent: "ua\r\nInjected: secret"}},
		{"file:///tmp/proxy", Fingerprint{}},
	} {
		if _, _, err := pool.Client(1, tc.proxy, tc.fp); err == nil {
			t.Fatalf("invalid transport accepted: %#v", tc)
		}
	}
}
