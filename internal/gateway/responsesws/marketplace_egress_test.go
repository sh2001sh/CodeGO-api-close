package responsesws

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceNativeSessionUsesPinnedPublicTLSConnection(t *testing.T) {
	var connections, lookups atomic.Int32
	upgrader := websocket.Upgrader{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer isolated-credential", r.Header.Get("Authorization"))
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		connections.Add(1)
		for i := 0; i < 2; i++ {
			_, body, err := conn.ReadMessage()
			if err != nil {
				return
			}
			require.Contains(t, string(body), "response.create")
			_ = conn.WriteJSON(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_isolated", "output": []any{}}})
		}
	}))
	defer server.Close()
	client := platformhttpx.NewMarketplaceHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
		lookups.Add(1)
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}, func(ctx context.Context, network, target string) (net.Conn, error) {
		if target != "93.184.216.34:443" {
			return nil, fmt.Errorf("unchecked target")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	})
	transport := client.Transport.(*platformhttpx.MarketplaceTransport)
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	previous := marketplaceWebSocketDial
	marketplaceWebSocketDial = transport.DialWebSocket
	defer func() { marketplaceWebSocketDial = previous }()
	session := NewSession()
	defer session.Close()
	session.dialer.NetDialContext = func(context.Context, string, string) (net.Conn, error) {
		t.Error("marketplace native session used administrator dialer")
		return nil, fmt.Errorf("unexpected dial")
	}
	require.NoError(t, session.BindRoute(9, 0, true))
	for i := 0; i < 2; i++ {
		resp, err := session.DoMarketplace(context.Background(), "https://example.com/v1/responses", http.Header{"Authorization": {"Bearer isolated-credential"}}, strings.NewReader(`{"model":"gpt-5","stream":true}`))
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "response.completed")
		_ = resp.Body.Close()
	}
	require.EqualValues(t, 1, connections.Load())
	require.EqualValues(t, 1, lookups.Load(), "the existing public TLS connection remains reusable")
}

func TestMarketplaceNativeSessionRejectsPrivateDNSBeforeHandshake(t *testing.T) {
	var dials atomic.Int32
	client := platformhttpx.NewMarketplaceHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}, nil
	}, func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, fmt.Errorf("unexpected dial")
	})
	previous := marketplaceWebSocketDial
	marketplaceWebSocketDial = client.Transport.(*platformhttpx.MarketplaceTransport).DialWebSocket
	defer func() { marketplaceWebSocketDial = previous }()
	session := NewSession()
	defer session.Close()
	session.dialer.NetDialContext = func(context.Context, string, string) (net.Conn, error) {
		t.Error("marketplace native session bypassed policy")
		return nil, fmt.Errorf("unexpected dial")
	}
	require.NoError(t, session.BindRoute(9, 0, true))
	_, err := session.DoMarketplace(context.Background(), "https://example.com/v1/responses?key=PRIVATE-CANARY", nil, strings.NewReader(`{"model":"gpt-5"}`))
	require.Error(t, err)
	require.True(t, CanFallbackToHTTP(err), "HTTP fallback is safe only before a response.create frame")
	require.NotContains(t, err.Error(), "PRIVATE-CANARY")
	require.NotContains(t, err.Error(), "10.0.0.1")
	require.Zero(t, dials.Load())
}

func TestMarketplaceNativeCancellationPreservesContext(t *testing.T) {
	previous := marketplaceWebSocketDial
	marketplaceWebSocketDial = func(ctx context.Context, _ string, _ http.Header) (*websocket.Conn, *http.Response, error) {
		return nil, nil, ctx.Err()
	}
	defer func() { marketplaceWebSocketDial = previous }()
	session := NewSession()
	defer session.Close()
	require.NoError(t, session.BindRoute(9, 0, true))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := session.DoMarketplace(ctx, "https://example.com/v1/responses", nil, strings.NewReader(`{"model":"gpt-5"}`))
	require.ErrorIs(t, err, context.Canceled)
}
