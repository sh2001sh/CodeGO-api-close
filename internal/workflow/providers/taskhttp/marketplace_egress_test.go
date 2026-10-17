package taskhttp

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
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/stretchr/testify/require"
)

type isolatedTaskAdaptor struct{ key string }

func (isolatedTaskAdaptor) BuildRequestURL(*relaycommon.RelayInfo) (string, error) {
	return "https://example.com/submit", nil
}
func (a isolatedTaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set(a.key, "isolated-credential")
	return nil
}

func TestMarketplaceTaskSubmitAndPollNeverFollowRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, key := range []string{"Authorization", "X-Goog-Api-Key"} {
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
						require.Equal(t, "private-task-body", string(body))
					}
					w.Header().Set("Location", "https://example.com/sink")
					w.WriteHeader(status)
				}))
				defer server.Close()
				client := platformhttpx.NewMarketplaceHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
					return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
				}, func(ctx context.Context, network, target string) (net.Conn, error) {
					if target != "93.184.216.34:443" {
						return nil, fmt.Errorf("unchecked dial target")
					}
					return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
				})
				defer client.CloseIdleConnections()
				client.Transport.(*platformhttpx.MarketplaceTransport).TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
				previous := marketplaceHTTPClient
				marketplaceHTTPClient = func(time.Duration) *http.Client { return client }
				defer func() { marketplaceHTTPClient = previous }()
				info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{MarketplaceEgress: true}}
				info.ChannelSetting.Proxy = "unsupported://must-not-use-admin-client"
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
				response, err := DoTaskAPIRequest(isolatedTaskAdaptor{key: key}, c, info, strings.NewReader("private-task-body"))
				require.NoError(t, err)
				require.Equal(t, status, response.StatusCode)
				_ = response.Body.Close()
				req, err := http.NewRequest(http.MethodGet, "https://example.com/poll", nil)
				require.NoError(t, err)
				req.Header.Set(key, "isolated-credential")
				response, err = DoUpstreamRequest(req, true, "unsupported://must-not-use-admin-client")
				require.NoError(t, err)
				require.Equal(t, status, response.StatusCode)
				_ = response.Body.Close()
				require.EqualValues(t, 2, served.Load())
				require.Zero(t, redirected.Load())
			})
		}
	}
}

func TestMarketplaceTaskRejectsMixedDNSAndPreservesCancellation(t *testing.T) {
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
	req, err := http.NewRequest(http.MethodGet, "https://example.com/poll?secret=PRIVATE-CANARY", nil)
	require.NoError(t, err)
	_, err = DoUpstreamRequest(req, true, "")
	require.ErrorIs(t, err, platformhttpx.ErrMarketplaceUpstreamAddress)
	require.NotContains(t, err.Error(), "PRIVATE-CANARY")
	require.Zero(t, dials.Load())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = DoUpstreamRequest(req.WithContext(ctx), true, "")
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, dials.Load())
}
