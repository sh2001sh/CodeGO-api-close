package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestMarketPoolReusesPinnedTLSAndSeparatesCredentialIdentities(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "example.com" || r.TLS.ServerName != "example.com" || r.Header.Get("Authorization") != "Bearer origin-secret" {
			t.Errorf("pooled request lost origin identity: %s %s", r.Host, r.TLS.ServerName)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer origin.Close()
	var dials, lookups atomic.Int64
	base := origin.Client().Transport.(*http.Transport).Clone()
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "8.8.8.8:443" {
			t.Errorf("pooled request bypassed address pinning: %s", address)
		}
		dials.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
	}
	pool := NewPool(TransportConfig{})
	defer pool.CloseIdle()
	selected := &http.Client{Transport: base}
	for i := 0; i < 50; i++ {
		client, err := pool.MarketClient(123, selected)
		if err != nil {
			t.Fatal(err)
		}
		client.Transport.(*marketTransport).lookup = func(context.Context, string, string) ([]netip.Addr, error) {
			lookups.Add(1)
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		}
		req, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/chat/completions", strings.NewReader("prompt"))
		req.Header.Set("Authorization", "Bearer origin-secret")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	if dials.Load() != 1 || lookups.Load() != 50 {
		t.Fatalf("50 requests should validate DNS 50 times and open one TLS connection: dials=%d lookups=%d", dials.Load(), lookups.Load())
	}
	t.Logf("50 authenticated requests: %d TLS connection, %d DNS policy checks", dials.Load(), lookups.Load())
	other, err := pool.MarketClient(456, selected)
	if err != nil {
		t.Fatal(err)
	}
	other.Transport.(*marketTransport).lookup = imageLookup
	req, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer origin-secret")
	response, err := other.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if dials.Load() != 2 {
		t.Fatal("different upstream credentials shared an origin connection")
	}
}

func TestMarketPoolConcurrentRequestsReuseValidatedOrigin(t *testing.T) {
	var requests, dials atomic.Int64
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, "ok")
	}))
	defer origin.Close()
	base := origin.Client().Transport.(*http.Transport).Clone()
	base.MaxIdleConns, base.MaxIdleConnsPerHost = 64, 64
	base.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		dials.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
	}
	selected := &http.Client{Transport: base}
	pool := NewPool(TransportConfig{})
	defer pool.CloseIdle()
	client, err := pool.MarketClient(1, selected)
	if err != nil {
		t.Fatal(err)
	}
	client.Transport.(*marketTransport).lookup = imageLookup
	const workers, calls = 16, 20
	results := make(chan error, workers)
	var running sync.WaitGroup
	for i := 0; i < workers; i++ {
		running.Add(1)
		go func() {
			defer running.Done()
			for j := 0; j < calls; j++ {
				client, err := pool.MarketClient(1, selected)
				if err != nil {
					results <- err
					return
				}
				response, err := client.Post("https://example.com/messages", "application/json", strings.NewReader("prompt"))
				if err != nil {
					results <- err
					return
				}
				body, readErr := io.ReadAll(response.Body)
				closeErr := response.Body.Close()
				if readErr != nil || closeErr != nil || string(body) != "ok" {
					results <- fmt.Errorf("pooled response lost: %q %v %v", body, readErr, closeErr)
					return
				}
			}
		}()
	}
	running.Wait()
	close(results)
	for err := range results {
		t.Error(err)
	}
	if requests.Load() != workers*calls || dials.Load() >= workers*calls/2 {
		t.Fatalf("concurrent marketplace requests did not reuse sockets: requests=%d TLS dials=%d", requests.Load(), dials.Load())
	}
	t.Logf("%d authenticated concurrent requests: %d TLS connections", requests.Load(), dials.Load())
}

type marketFixtureFactory struct {
	created []*marketFixtureTransport
}

func (f *marketFixtureFactory) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unvalidated base transport must never be used")
}

func (f *marketFixtureFactory) PublicMarketTransport(_ string, _ netip.Addr) (http.RoundTripper, error) {
	transport := &marketFixtureTransport{}
	f.created = append(f.created, transport)
	return transport, nil
}

type marketFixtureTransport struct {
	closed atomic.Int64
}

func (*marketFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
}

func (t *marketFixtureTransport) CloseIdleConnections() { t.closed.Add(1) }

func TestMarketCachedConnectionCannotBypassDNSRebindingOrAddressChange(t *testing.T) {
	factory := &marketFixtureFactory{}
	client, err := MarketClient(&http.Client{Transport: factory})
	if err != nil {
		t.Fatal(err)
	}
	transport := client.Transport.(*marketTransport)
	address := netip.MustParseAddr("8.8.8.8")
	transport.lookup = func(context.Context, string, string) ([]netip.Addr, error) { return []netip.Addr{address}, nil }
	for _, raw := range []string{"8.8.8.8", "8.8.4.4", "8.8.8.8"} {
		address = netip.MustParseAddr(raw)
		response, err := client.Post("https://example.com/v1/messages", "application/json", strings.NewReader("prompt"))
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
	}
	if len(factory.created) != 2 {
		t.Fatal("changed validated public IP reused the previous pinned transport")
	}
	address = netip.MustParseAddr("127.0.0.1")
	if _, err := client.Get("https://example.com/v1/messages"); !errors.Is(err, ErrMarketTransportPolicy) || len(factory.created) != 2 {
		t.Fatalf("cached transport bypassed current DNS policy: %v", err)
	}
	for _, created := range factory.created {
		if created.closed.Load() != 0 {
			t.Fatal("ordinary response close discarded reusable connection pool")
		}
	}
	client.CloseIdleConnections()
	for _, created := range factory.created {
		if created.closed.Load() != 1 {
			t.Fatal("explicit close did not drain cached origin pools")
		}
	}
}

func TestMarketOriginPoolEvictionDrainsLateActiveResponses(t *testing.T) {
	factory := &marketFixtureFactory{}
	client, _ := MarketClient(&http.Client{Transport: factory})
	transport := client.Transport.(*marketTransport)
	transport.lookup = imageLookup
	first, err := client.Get("https://origin0.example/messages")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= marketOriginsPerClient; i++ {
		response, err := client.Get("https://origin" + strconv.Itoa(i) + ".example/messages")
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
	}
	if len(transport.pinned) != marketOriginsPerClient || factory.created[0].closed.Load() != 1 {
		t.Fatal("origin cache was not bounded or eviction did not close idle sockets")
	}
	_ = first.Body.Close()
	if factory.created[0].closed.Load() != 2 {
		t.Fatal("active response resurrected an evicted idle pool")
	}
}

func TestMarketClientPoolBoundsAndRetiresChangedTransport(t *testing.T) {
	pool := NewPool(TransportConfig{})
	defer pool.CloseIdle()
	firstFactory, nextFactory := &marketFixtureFactory{}, &marketFixtureFactory{}
	first, _ := pool.MarketClient(1, &http.Client{Transport: firstFactory})
	firstTransport := first.Transport.(*marketTransport)
	firstTransport.lookup = imageLookup
	response, err := first.Get("https://example.com/messages")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.MarketClient(1, &http.Client{Transport: nextFactory}); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if !firstTransport.retired || firstFactory.created[0].closed.Load() != 2 || len(pool.marketClients) != 1 {
		t.Fatal("credential proxy/TLS identity change did not retire previous origin pools")
	}
	for id := int64(2); id <= marketClientsPerPool+2; id++ {
		if _, err := pool.MarketClient(id, &http.Client{Transport: nextFactory}); err != nil {
			t.Fatal(err)
		}
	}
	if len(pool.marketClients) != marketClientsPerPool {
		t.Fatal("credential marketplace cache exceeded its bound")
	}
}

func TestMarketPinnedPoolSeparatesDynamicProxyCredentials(t *testing.T) {
	firstProxy, _ := url.Parse("http://first:secret@proxy.example:3128")
	secondProxy, _ := url.Parse("http://second:secret@proxy.example:3128")
	selectedProxy := firstProxy
	transport := &marketTransport{base: &http.Transport{Proxy: func(*http.Request) (*url.URL, error) { return selectedProxy, nil }}}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com/messages", nil)
	first, releaseFirst, err := transport.acquire(req, netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	releaseFirst()
	selectedProxy = secondProxy
	second, releaseSecond, err := transport.acquire(req, netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	releaseSecond()
	if first == second || len(transport.pinned) != 2 {
		t.Fatal("dynamic proxy authentication identities shared a pinned pool")
	}
	selectedProxy = firstProxy
	again, releaseAgain, err := transport.acquire(req, netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	releaseAgain()
	if again != first {
		t.Fatal("unchanged proxy authentication identity did not reuse its pool")
	}
	transport.CloseIdleConnections()
}

func BenchmarkMarketTLSRequests(b *testing.B) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer origin.Close()
	for _, pooled := range []bool{false, true} {
		name := "per_request_transport"
		if pooled {
			name = "pooled_pinned_transport"
		}
		b.Run(name, func(b *testing.B) {
			var dials atomic.Int64
			base := origin.Client().Transport.(*http.Transport).Clone()
			base.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
				dials.Add(1)
				return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
			}
			selected := &http.Client{Transport: base}
			pool := NewPool(TransportConfig{})
			defer pool.CloseIdle()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var client *http.Client
				var err error
				if pooled {
					client, err = pool.MarketClient(1, selected)
				} else {
					client, err = MarketClient(selected)
				}
				if err != nil {
					b.Fatal(err)
				}
				client.Transport.(*marketTransport).lookup = imageLookup
				response, err := client.Post("https://example.com/messages", "application/json", strings.NewReader("prompt"))
				if err != nil {
					b.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				if !pooled {
					client.CloseIdleConnections()
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(dials.Load())/float64(b.N), "TLS_dials/op")
		})
	}
}
