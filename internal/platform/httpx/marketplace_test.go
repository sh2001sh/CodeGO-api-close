package httpx

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceWebSocketPinsDNSAndBlocksRebinding(t *testing.T) {
	var served, lookups, dials atomic.Int32
	upgrader := websocket.Upgrader{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Add(1)
		require.Equal(t, "Bearer isolated-test-key", r.Header.Get("Authorization"))
		conn, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()
	client := NewMarketplaceHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
		if lookups.Add(1) == 1 {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}, func(ctx context.Context, network, target string) (net.Conn, error) {
		dials.Add(1)
		if target != "93.184.216.34:443" {
			return nil, fmt.Errorf("unchecked dial target")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	})
	transport := client.Transport.(*MarketplaceTransport)
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	conn, _, err := transport.DialWebSocket(context.Background(), "wss://example.com/v1/responses", http.Header{"Authorization": {"Bearer isolated-test-key"}})
	require.NoError(t, err)
	_ = conn.Close()
	_, _, err = transport.DialWebSocket(context.Background(), "wss://example.com/v1/responses", nil)
	require.ErrorIs(t, err, ErrMarketplaceUpstreamAddress)
	require.EqualValues(t, 2, lookups.Load())
	require.EqualValues(t, 1, dials.Load())
	require.EqualValues(t, 1, served.Load())
}

func TestMarketplaceWebSocketRejectsUnsafeAddressesBeforeDial(t *testing.T) {
	for _, target := range []string{
		"ws://example.com/v1/realtime", "wss://user:pass@example.com/v1/realtime",
		"wss://example.com:0/v1/realtime", "wss://example.com:65536/v1/realtime", "wss://127.0.0.1:8080/v1/realtime",
		"wss://[::1]/v1/realtime", "wss://example.com/v1/realtime#fragment",
	} {
		t.Run(target, func(t *testing.T) {
			var dials atomic.Int32
			client := NewMarketplaceHTTPClient(nil, func(context.Context, string, string) (net.Conn, error) {
				dials.Add(1)
				return nil, fmt.Errorf("unexpected network access")
			})
			_, _, err := client.Transport.(*MarketplaceTransport).DialWebSocket(context.Background(), target, nil)
			require.ErrorIs(t, err, ErrMarketplaceUpstreamAddress)
			require.Zero(t, dials.Load())
		})
	}
	client := NewMarketplaceHTTPClient(nil, nil)
	_, _, err := client.Transport.(*MarketplaceTransport).DialWebSocket(context.Background(), "wss://example.com/v1/realtime", http.Header{"Host": {"127.0.0.1"}})
	require.ErrorIs(t, err, ErrMarketplaceUpstreamAddress)
}

func TestMarketplaceURLAcceptsValidCustomPorts(t *testing.T) {
	for _, endpoint := range []string{
		"https://178.83.203.233:8080/v1", "https://example.com:1/v1",
		"https://example.com:80/v1", "https://example.com:3000/v1",
		"https://example.com:65535/v1", "https://[2001:4860:4860::8888]:8080/v1",
	} {
		parsed, err := url.Parse(endpoint)
		require.NoError(t, err)
		require.NoError(t, ValidateMarketplaceURLFormat(parsed), endpoint)
	}
	for _, port := range []string{"0", "65536", "9999999999999999999999999"} {
		parsed, err := url.Parse("https://example.com:" + port)
		require.NoError(t, err)
		require.Error(t, ValidateMarketplaceURLFormat(parsed))
	}
}

func TestMarketplaceCustomPortHTTPSAndWebSocket(t *testing.T) {
	for _, port := range []string{"1", "8080", "65535"} {
		t.Run(port, func(t *testing.T) {
			upgrader := websocket.Upgrader{}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "example.com:"+port, r.Host)
				if r.URL.Path == "/socket" {
					conn, err := upgrader.Upgrade(w, r, nil)
					if err == nil {
						_ = conn.Close()
					}
					return
				}
				_, _ = io.WriteString(w, "custom-port-ok")
			}))
			defer server.Close()
			dials := 0
			client := NewMarketplaceHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
			}, func(ctx context.Context, network, target string) (net.Conn, error) {
				dials++
				require.Equal(t, "93.184.216.34:"+port, target)
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			})
			defer client.CloseIdleConnections()
			transport := client.Transport.(*MarketplaceTransport)
			transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			resp, err := client.Get("https://example.com:" + port + "/models")
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			_ = resp.Body.Close()
			require.Equal(t, "custom-port-ok", string(body))
			conn, _, err := transport.DialWebSocket(context.Background(), "wss://example.com:"+port+"/socket", nil)
			require.NoError(t, err)
			_ = conn.Close()
			require.Equal(t, 2, dials)
		})
	}
}

func TestMarketplaceWebSocketNeverFollowsRedirectOrProxy(t *testing.T) {
	var redirected, proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { proxied.Add(1) }))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sink" {
			redirected.Add(1)
			return
		}
		require.Equal(t, "isolated-test-key", r.Header.Get("x-api-key"))
		w.Header().Set("Location", "wss://example.com/sink")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := NewMarketplaceHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}, func(ctx context.Context, network, target string) (net.Conn, error) {
		if target != "93.184.216.34:443" {
			return nil, fmt.Errorf("unchecked target")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	})
	transport := client.Transport.(*MarketplaceTransport)
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	_, response, err := transport.DialWebSocket(context.Background(), "wss://example.com/start", http.Header{"X-Api-Key": {"isolated-test-key"}})
	require.Error(t, err)
	require.Equal(t, http.StatusTemporaryRedirect, response.StatusCode)
	_ = response.Body.Close()
	require.Zero(t, redirected.Load())
	require.Zero(t, proxied.Load())
	// The same real TLS endpoint must not receive an HTTP request when its
	// certificate is untrusted, regardless of the relay's global TLS setting.
	transport.TLSClientConfig = &tls.Config{}
	_, _, err = transport.DialWebSocket(context.Background(), "wss://example.com/start", nil)
	require.Error(t, err)
}

func TestMarketplaceHTTPClientRetainsHeaderBudgetAndConnectionReuse(t *testing.T) {
	first := GetMarketplaceHTTPClientWithResponseHeaderTimeout(12 * time.Second)
	second := GetMarketplaceHTTPClientWithResponseHeaderTimeout(13 * time.Second)
	require.Same(t, first.Transport, second.Transport)
	transport := first.Transport.(*MarketplaceTransport)
	require.Equal(t, 15*time.Second, transport.ResponseHeaderTimeout)
	require.Nil(t, transport.Proxy)
	require.Nil(t, transport.TLSClientConfig)
	previous := platformconfig.RelayTimeout
	platformconfig.RelayTimeout = 37
	t.Cleanup(func() { platformconfig.RelayTimeout = previous })
	configured := GetMarketplaceHTTPClientWithResponseHeaderTimeout(12 * time.Second)
	require.Equal(t, 37*time.Second, configured.Timeout)
	require.Same(t, first.Transport, configured.Transport)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://127.0.0.1/v1/responses", strings.NewReader("{}"))
	require.NoError(t, err)
	response, err := first.Do(req)
	if response != nil {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	require.Error(t, err)
}
