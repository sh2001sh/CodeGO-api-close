package httpx

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchMediaPinsPublicDNSAndSupportsBinaryMedia(t *testing.T) {
	transport, dials := imageTestTransport(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "image.example" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("credential or hostname leaked: %s %v", r.Host, r.Header)
		}
		_, _ = w.Write([]byte("audio-media-bytes"))
	})
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse("http://image.example")
	jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "must-not-leak"}})
	lookups := 0
	lookup := func(ctx context.Context, network, host string) ([]netip.Addr, error) {
		lookups++
		if lookups > 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return imageLookup(ctx, network, host)
	}
	client := &http.Client{Transport: transport, Jar: jar}
	data, err := fetchMedia(context.Background(), "http://image.example/audio", MediaFetchConfig{Client: client}, lookup)
	if err != nil || string(data) != "audio-media-bytes" || lookups != 1 || dials.Load() != 1 || client.Jar != jar {
		t.Fatalf("binary/pinned download failed: %s %v lookups=%d dials=%d", data, err, lookups, dials.Load())
	}
}

func TestFetchMediaRejectsPrivateAndMixedDNSBeforeDial(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1/private", "http://[::1]/private", "http://169.254.169.254/latest/meta-data", "http://10.0.0.1/private", "file:///private", "https://user:password@example.com/file", "https://example.com/file#fragment"} {
		if _, err := FetchMedia(context.Background(), raw, MediaFetchConfig{}); err == nil {
			t.Errorf("unsafe media address accepted: %s", raw)
		}
	}
	transport, dials := imageTestTransport(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, nil
	}
	if _, err := fetchMedia(context.Background(), "http://image.example/media", MediaFetchConfig{Client: &http.Client{Transport: transport}}, lookup); err == nil || dials.Load() != 0 {
		t.Fatalf("mixed DNS reached network: %v dials=%d", err, dials.Load())
	}
}

func TestFetchMediaTrustedOriginIsExplicitAndDoesNotFollowRedirectOrSendCookies(t *testing.T) {
	var leaked atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { leaked.Add(1); _, _ = w.Write([]byte("private")) }))
	defer sink.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("download forwarded credentials")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, sink.URL, http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("self-hosted-media"))
	}))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(server.URL)
	jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "secret"}})
	cfg := MediaFetchConfig{Client: &http.Client{Jar: jar}, TrustedOrigin: server.URL + "/v1"}
	data, err := FetchMedia(context.Background(), server.URL+"/media", cfg)
	if err != nil || string(data) != "self-hosted-media" {
		t.Fatalf("configured same-origin upstream blocked: %s %v", data, err)
	}
	for _, raw := range []string{server.URL + "/redirect", sink.URL, strings.Replace(server.URL, "http:", "https:", 1)} {
		if _, err := FetchMedia(context.Background(), raw, cfg); err == nil {
			t.Errorf("redirect/other origin accepted: %s", raw)
		}
	}
	if leaked.Load() != 0 {
		t.Fatalf("untrusted private result was fetched %d times", leaked.Load())
	}
}

func TestFetchMediaEnforcesSizeAndCancellation(t *testing.T) {
	for _, mode := range []string{"header", "body", "empty"} {
		t.Run(mode, func(t *testing.T) {
			transport, _ := imageTestTransport(t, func(w http.ResponseWriter, _ *http.Request) {
				switch mode {
				case "header":
					w.Header().Set("Content-Length", "2048")
				case "body":
					_, _ = w.Write([]byte(strings.Repeat("x", 1025)))
				}
			})
			if _, err := fetchMedia(context.Background(), "http://image.example/media", MediaFetchConfig{Client: &http.Client{Transport: transport}, MaxBytes: 1024}, imageLookup); err == nil {
				t.Fatal("invalid size accepted")
			}
		})
	}
	transport, _ := imageTestTransport(t, func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	if _, err := fetchMedia(context.Background(), "http://image.example/media", MediaFetchConfig{Client: &http.Client{Transport: transport}, Timeout: 20 * time.Millisecond}, imageLookup); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout lost: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FetchMedia(ctx, "http://image.example/media", MediaFetchConfig{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel lost: %v", err)
	}
}

func TestFetchMediaPreservesCustomPinnedTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("media")) }))
	defer server.Close()
	factory := &pinnedImageFactory{target: server.Listener.Addr().String()}
	data, err := fetchMedia(context.Background(), "http://image.example/media", MediaFetchConfig{Client: &http.Client{Transport: factory}}, imageLookup)
	if err != nil || string(data) != "media" || factory.host != "image.example" || factory.address != netip.MustParseAddr("8.8.8.8") {
		t.Fatalf("selected pinned TLS/proxy lost: %s %v factory=%+v", data, err, factory)
	}
	if _, err := fetchMedia(context.Background(), "http://image.example/media", MediaFetchConfig{Client: &http.Client{Transport: unsupportedImageTransport{}}}, imageLookup); !errors.Is(err, ErrImageTransportPolicy) {
		t.Fatalf("unsupported transport silently replaced: %v", err)
	}
}

func TestFetchMediaPreservesProxyAndPinsExternalOrigin(t *testing.T) {
	var tunnels atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "8.8.8.8:80" {
			t.Errorf("external media proxy would resolve unpinned DNS: %s %s", r.Method, r.Host)
			w.WriteHeader(400)
			return
		}
		tunnels.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		origin, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil || origin.Host != "image.example" || origin.Header.Get("Authorization") != "" {
			t.Errorf("external media virtual host/credential policy lost: %+v %v", origin, err)
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Length: 5\r\n\r\nmedia")
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	data, err := fetchMedia(context.Background(), "http://image.example/media", MediaFetchConfig{Client: &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}}}, imageLookup)
	if err != nil || string(data) != "media" || tunnels.Load() != 1 {
		t.Fatalf("proxy/pinning failed: %s %v tunnels=%d", data, err, tunnels.Load())
	}
}
