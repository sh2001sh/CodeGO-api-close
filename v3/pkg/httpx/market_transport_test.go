package httpx

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMarketTransportRejectsEveryPrivateDNSAnswerBeforeSendingSecrets(t *testing.T) {
	var calls atomic.Int64
	base := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		return nil, errors.New("should not dial")
	}}
	for _, private := range []string{"127.0.0.1", "::ffff:10.0.0.1", "169.254.169.254", "100.64.0.1", "2001:db8::1", "64:ff9b::7f00:1"} {
		transport := &marketTransport{base: base, lookup: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr(private)}, nil
		}}
		r := httptest.NewRequest(http.MethodPost, "https://example.com/v1/chat/completions", strings.NewReader("private prompt"))
		r.Header.Set("Authorization", "Bearer private-secret")
		if _, err := transport.RoundTrip(r); !errors.Is(err, ErrMarketTransportPolicy) {
			t.Fatalf("private answer accepted %s: %v", private, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unsafe DNS reached network")
	}
}

func TestMarketTransportPinsDNSAndPreservesAuthenticatedPOST(t *testing.T) {
	var calls atomic.Int64
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer retained" || string(body) != "prompt" || r.Host != "example.com" || r.TLS.ServerName != "example.com" {
			t.Errorf("relay identity lost: %s %s %s %s", r.Method, r.Host, r.TLS.ServerName, body)
		}
		w.Header().Set("Location", "https://127.0.0.1/private")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	selected := origin.Client()
	standard := selected.Transport.(*http.Transport).Clone()
	standard.TLSClientConfig.ServerName = "" // the wrapper must select origin SNI
	standard.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "8.8.8.8:443" {
			t.Errorf("hostname resolved again instead of pinned address: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
	}
	selected.Transport = standard
	client, err := MarketClient(selected)
	if err != nil {
		t.Fatal(err)
	}
	client.Transport.(*marketTransport).lookup = imageLookup
	r, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/chat/completions", strings.NewReader("prompt"))
	r.Header.Set("Authorization", "Bearer retained")
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect || calls.Load() != 1 || r.URL.Host != "example.com" || selected.CheckRedirect != nil {
		t.Fatal("redirect followed or selected ordinary client mutated")
	}
}

func TestMarketTransportRejectsUnsafeSchemePortOrTLSBypass(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://example.com:8080", "https://user:secret@example.com", "https://example.com#fragment"} {
		client, _ := MarketClient(&http.Client{Transport: &http.Transport{}})
		client.Transport.(*marketTransport).lookup = imageLookup
		r, _ := http.NewRequest(http.MethodPost, raw, nil)
		if _, err := client.Do(r); !errors.Is(err, ErrMarketTransportPolicy) {
			t.Fatalf("unsafe URL %s: %v", raw, err)
		}
	}
	for _, standard := range []*http.Transport{{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, {DialTLSContext: func(context.Context, string, string) (net.Conn, error) { return nil, nil }}} {
		client, _ := MarketClient(&http.Client{Transport: standard})
		client.Transport.(*marketTransport).lookup = imageLookup
		r, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
		if _, err := client.Do(r); !errors.Is(err, ErrMarketTransportPolicy) {
			t.Fatalf("unsafe TLS configuration: %v", err)
		}
	}
}

func TestMarketClientPreservesPublicImageFetchingWithoutOriginCredentials(t *testing.T) {
	var calls atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.Host != "example.com" || r.Header.Get("Authorization") != "" || r.Header.Get("X-Api-Key") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("origin credentials or incorrect method reached image source")
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imagePNG)
	}))
	defer origin.Close()
	standard := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "8.8.8.8:80" {
			t.Errorf("image host was not pinned: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
	}}
	client, err := MarketClient(&http.Client{Transport: standard})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fetchImage(context.Background(), "http://example.com/image", ImageFetchConfig{Transport: client.Transport}, imageLookup)
	if err != nil || len(result.Data) != len(imagePNG) {
		t.Fatalf("market multimodal image fetch failed: %v", err)
	}
	pinned, err := client.Transport.(PinnedImageTransport).PublicImageTransport("example.com", netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	r, _ := http.NewRequest(http.MethodGet, "http://example.com/image", nil)
	r.Header = http.Header{"Authorization": {"Bearer origin"}, "X-Api-Key": {"origin"}, "Cookie": {"origin"}, "Proxy-Authorization": {"proxy"}}
	response, err := pinned.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if calls.Load() != 2 {
		t.Fatal("expected two isolated pinned image requests")
	}
	if _, err := client.Transport.(PinnedImageTransport).PublicImageTransport("example.com", netip.MustParseAddr("127.0.0.1")); !errors.Is(err, ErrImageTransportPolicy) {
		t.Fatalf("private image accepted through market factory: %v", err)
	}
}

func TestMarketClientPreservesRealUpgradeDuplexAndRejectsHostOverride(t *testing.T) {
	var calls atomic.Int64
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Host != "example.com" || r.TLS.ServerName != "example.com" || r.Header.Get("Upgrade") != "websocket" {
			t.Error("upgrade lost pinned origin or protocol")
		}
		conn, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = buffered.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		if err := buffered.Flush(); err != nil {
			t.Error(err)
			return
		}
		data := make([]byte, 4)
		if _, err := io.ReadFull(buffered, data); err != nil || string(data) != "ping" {
			t.Errorf("upgrade cannot receive client bytes: %q %v", data, err)
			return
		}
		_, _ = io.WriteString(conn, "pong")
	}))
	defer origin.Close()
	selected := origin.Client()
	standard := selected.Transport.(*http.Transport).Clone()
	standard.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "8.8.8.8:443" {
			t.Errorf("upgrade destination was not pinned: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
	}
	selected.Transport = standard
	client, err := MarketClient(selected)
	if err != nil {
		t.Fatal(err)
	}
	client.Transport.(*marketTransport).lookup = imageLookup
	r, _ := http.NewRequest(http.MethodGet, "https://example.com/realtime", nil)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	r.Host = "private.internal"
	if _, err := client.Do(r); !errors.Is(err, ErrMarketTransportPolicy) || calls.Load() != 0 {
		t.Fatalf("host override reached network: calls=%d err=%v", calls.Load(), err)
	}
	r.Host = "example.com"
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	duplex, ok := response.Body.(io.ReadWriteCloser)
	if response.StatusCode != http.StatusSwitchingProtocols || !ok {
		t.Fatal("market wrapper discarded real upgrade duplex body")
	}
	if _, err := io.WriteString(duplex, "ping"); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4)
	if _, err := io.ReadFull(duplex, data); err != nil || string(data) != "pong" {
		t.Fatalf("market upgrade cannot receive server bytes: %q %v", data, err)
	}
	ordinary := &marketBody{ReadCloser: io.NopCloser(strings.NewReader("ordinary"))}
	if _, err := ordinary.Write([]byte("not supported")); !errors.Is(err, ErrMarketTransportPolicy) {
		t.Fatalf("ordinary response pretended to support duplex writing: %v", err)
	}
}
