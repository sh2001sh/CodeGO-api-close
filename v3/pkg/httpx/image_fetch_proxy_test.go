package httpx

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFetchImageHTTPSProxyRetainsProxyAndOriginTLSIdentity(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "example.com" || r.TLS.ServerName != "example.com" {
			t.Errorf("origin host/SNI lost through HTTPS proxy: %s %s", r.Host, r.TLS.ServerName)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imagePNG)
	}))
	defer origin.Close()
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "8.8.8.8:443" {
			t.Errorf("HTTPS proxy origin was not pinned: %s %s", r.Method, r.Host)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = client.Close() }()
		server, err := net.Dial("tcp", origin.Listener.Addr().String())
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = server.Close() }()
		_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		done := make(chan struct{})
		go func() { _, _ = io.Copy(server, client); close(done) }()
		_, _ = io.Copy(client, server)
		_ = client.Close()
		<-done
	}))
	defer proxy.Close()
	roots := x509.NewCertPool()
	for _, server := range []*httptest.Server{origin, proxy} {
		cert, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
		if err != nil {
			t.Fatal(err)
		}
		roots.AddCert(cert)
	}
	proxyURL, _ := url.Parse(proxy.URL)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots}}
	if _, err := fetchImage(context.Background(), "https://example.com/image", ImageFetchConfig{Transport: transport}, imageLookup); err != nil {
		t.Fatalf("separate proxy/origin TLS verification failed: %v", err)
	}
}
