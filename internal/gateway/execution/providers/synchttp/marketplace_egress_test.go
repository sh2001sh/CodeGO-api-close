package synchttp

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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/stretchr/testify/require"
)

type marketplaceCredentialAdaptor struct {
	url, key string
}

func (a marketplaceCredentialAdaptor) GetRequestURL(*relaycommon.RelayInfo) (string, error) {
	return a.url, nil
}
func (a marketplaceCredentialAdaptor) SetupRequestHeader(_ *gin.Context, h *http.Header, _ *relaycommon.RelayInfo) error {
	h.Set(a.key, "isolated-credential")
	return nil
}

func marketplaceRelayInfo() *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{MarketplaceSourceType: "marketplace_user", MarketplaceOwnerID: 7, MarketplaceGroupID: "group-7", ChannelMeta: &relaycommon.ChannelMeta{}}
	// A regression to the administrator HTTP client must fail locally, never
	// resolve the public fixture hostname through an uncontrolled transport.
	info.ChannelSetting.Proxy = "unsupported://isolated-test"
	return info
}

func TestMarketplaceRelayRealRequestsNeverFollowRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, key := range []string{"Authorization", "X-Api-Key", "Api-Key", "X-Goog-Api-Key"} {
			t.Run(fmt.Sprintf("%d/%s", status, key), func(t *testing.T) {
				var served, redirected atomic.Int32
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/sink" {
						redirected.Add(1)
						return
					}
					served.Add(1)
					require.Equal(t, "isolated-credential", r.Header.Get(key))
					if r.Method == http.MethodPost {
						body, err := io.ReadAll(r.Body)
						require.NoError(t, err)
						require.Equal(t, "private-request-body", string(body))
					}
					w.Header().Set("Location", "https://example.com/sink")
					w.WriteHeader(status)
				}))
				defer server.Close()
				client := platformhttpx.NewMarketplaceHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
					return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
				}, func(ctx context.Context, network, target string) (net.Conn, error) {
					if target != "93.184.216.34:443" {
						return nil, fmt.Errorf("unchecked numeric target")
					}
					return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
				})
				defer client.CloseIdleConnections()
				client.Transport.(*platformhttpx.MarketplaceTransport).TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
				previous := marketplaceHTTPClient
				marketplaceHTTPClient = func(time.Duration) *http.Client { return client }
				defer func() { marketplaceHTTPClient = previous }()
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				adaptor := marketplaceCredentialAdaptor{url: "https://example.com/start", key: key}
				for _, mode := range []string{"post", "follow-up-get", "form"} {
					var resp *http.Response
					var err error
					info := marketplaceRelayInfo()
					switch mode {
					case "follow-up-get":
						resp, err = DoAPIRequestAt(adaptor, ctx, info, http.MethodGet, adaptor.url, nil)
					case "form":
						resp, err = DoFormAPIRequest(adaptor, ctx, info, strings.NewReader("private-request-body"))
					default:
						resp, err = DoAPIRequest(adaptor, ctx, info, strings.NewReader("private-request-body"))
					}
					require.NoError(t, err)
					require.Equal(t, status, resp.StatusCode)
					_ = resp.Body.Close()
				}
				require.EqualValues(t, 3, served.Load())
				require.Zero(t, redirected.Load())
			})
		}
	}
}

func TestMarketplaceRelayDNSRebindingIsBlockedWhileAdministratorLoopbackWorks(t *testing.T) {
	var served atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { served.Add(1); w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	_, err := DoAPIRequest(contextAwareRequestAdaptor{url: server.URL}, ctx, marketplaceRelayInfo(), strings.NewReader("private-body"))
	require.Error(t, err)
	require.Zero(t, served.Load())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	resp, err := DoAPIRequest(contextAwareRequestAdaptor{url: server.URL}, ctx, info, strings.NewReader("{}"))
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.EqualValues(t, 1, served.Load())
	var dials atomic.Int32
	client := platformhttpx.NewMarketplaceHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}, func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, fmt.Errorf("unexpected dial")
	})
	previous := marketplaceHTTPClient
	marketplaceHTTPClient = func(time.Duration) *http.Client { return client }
	defer func() { marketplaceHTTPClient = previous }()
	_, err = DoAPIRequest(contextAwareRequestAdaptor{url: "https://example.com/v1/responses"}, ctx, marketplaceRelayInfo(), strings.NewReader("private-body"))
	require.Error(t, err)
	require.Zero(t, dials.Load())
	require.NotContains(t, err.Error(), "example.com")
}

func TestMarketplaceRealtimeUsesMandatoryEgress(t *testing.T) {
	var served atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served.Add(1) }))
	defer server.Close()
	previous := websocket.DefaultDialer
	websocket.DefaultDialer = &websocket.Dialer{NetDialContext: func(context.Context, string, string) (net.Conn, error) {
		t.Error("marketplace realtime used administrator dialer")
		return nil, fmt.Errorf("unexpected dial")
	}}
	defer func() { websocket.DefaultDialer = previous }()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	_, err := DoWSSRequest(contextAwareRequestAdaptor{url: "ws" + strings.TrimPrefix(server.URL, "http")}, ctx, marketplaceRelayInfo(), nil)
	require.Error(t, err)
	require.Zero(t, served.Load())
}

func TestMarketplaceRelayCancellationStopsBeforeDial(t *testing.T) {
	var dials atomic.Int32
	client := platformhttpx.NewMarketplaceHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}, func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, fmt.Errorf("unexpected dial")
	})
	previous := marketplaceHTTPClient
	marketplaceHTTPClient = func(time.Duration) *http.Client { return client }
	defer func() { marketplaceHTTPClient = previous }()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestContext)
	_, err := DoAPIRequest(contextAwareRequestAdaptor{url: "https://example.com/v1/responses?key=PRIVATE-CANARY"}, ctx, marketplaceRelayInfo(), strings.NewReader("private-body"))
	// The gateway's existing hidden-error option intentionally replaces the
	// outward error. Cancellation remains authoritative on the request context.
	require.Error(t, err)
	require.ErrorIs(t, ctx.Request.Context().Err(), context.Canceled)
	require.NotContains(t, err.Error(), "PRIVATE-CANARY")
	require.Zero(t, dials.Load())
}
