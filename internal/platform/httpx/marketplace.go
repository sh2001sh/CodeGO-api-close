package httpx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
)

var ErrMarketplaceUpstreamAddress = errors.New("上游请求地址不符合安全要求")

var marketplaceHTTPClients = struct {
	sync.Mutex
	clients map[time.Duration]*http.Client
}{clients: make(map[time.Duration]*http.Client)}

// GetMarketplaceHTTPClientWithResponseHeaderTimeout uses the mandatory public
// destination policy independently of administrator proxy, TLS or fetch settings.
func GetMarketplaceHTTPClientWithResponseHeaderTimeout(timeout time.Duration) *http.Client {
	timeout = bucketResponseHeaderTimeout(timeout)
	marketplaceHTTPClients.Lock()
	defer marketplaceHTTPClients.Unlock()
	client := marketplaceHTTPClients.clients[timeout]
	if client == nil {
		client = NewMarketplaceHTTPClient(nil, nil)
		client.Transport.(*MarketplaceTransport).ResponseHeaderTimeout = timeout
		marketplaceHTTPClients.clients[timeout] = client
	}
	// Keep the shared connection pool while respecting the current relay's
	// overall timeout. Verification callers keep their own context budget.
	configured := *client
	if platformconfig.RelayTimeout > 0 {
		configured.Timeout = time.Duration(platformconfig.RelayTimeout) * time.Second
	}
	return &configured
}

// MarketplaceTransport validates each URL and dials only checked numeric IPs.
type MarketplaceTransport struct{ *http.Transport }

func (t *MarketplaceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if ValidateMarketplaceURLFormat(req.URL) != nil || (req.Host != "" && req.Host != req.URL.Host) {
		return nil, ErrMarketplaceUpstreamAddress
	}
	return t.Transport.RoundTrip(req)
}

// ValidateMarketplaceURLFormat limits owner endpoints to credential-free HTTPS.
func ValidateMarketplaceURLFormat(parsed *url.URL) error {
	if parsed == nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || parsed.Fragment != "" {
		return errors.New("Base URL 必须是有效的 HTTPS 地址")
	}
	if parsed.Port() != "" && parsed.Port() != "443" && parsed.Port() != "8443" {
		return errors.New("Base URL 仅允许 443 或 8443 端口")
	}
	return nil
}

// NewMarketplaceHTTPClient creates a strict client. Nil dependencies use the
// system resolver and TCP dialer; explicit dependencies permit isolated tests.
func NewMarketplaceHTTPClient(
	lookup func(context.Context, string) ([]net.IPAddr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) *http.Client {
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	if dial == nil {
		dial = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	transport := &http.Transport{
		// Marketplace destinations are supplied by untrusted channel owners.
		// A proxy or the relay's optional insecure TLS setting would bypass this policy.
		Proxy: nil, TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout: 90 * time.Second, MaxIdleConns: 64, MaxIdleConnsPerHost: 4,
		ForceAttemptHTTP2: true,
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || (port != "443" && port != "8443") {
			return nil, ErrMarketplaceUpstreamAddress
		}
		var addresses []net.IPAddr
		if ip := net.ParseIP(host); ip != nil {
			addresses = []net.IPAddr{{IP: ip}}
		} else {
			resolveCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			addresses, err = lookup(resolveCtx, host)
			if err != nil || len(addresses) == 0 {
				return nil, errors.New("上游域名无法解析")
			}
		}
		// Reject mixed public/private answers before opening any connection.
		for _, address := range addresses {
			if address.Zone != "" || !platformsecurity.IsPublicUpstreamIP(address.IP) {
				return nil, ErrMarketplaceUpstreamAddress
			}
		}
		for _, address := range addresses {
			// Dial the checked numeric IP, never resolve the hostname a second time.
			// The original request host remains intact for TLS SNI and certificate checks.
			conn, err := dial(ctx, network, net.JoinHostPort(address.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, errors.New("无法连接上游服务")
	}
	return &http.Client{
		Transport: &MarketplaceTransport{transport},
		// Return the original 3xx response; no Location is ever fetched, even on
		// the same host. This also prevents custom provider credentials/body leaks.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// DialWebSocket applies the same checked-IP dialer and TLS verification to wss.
// Gorilla does not follow redirects; the caller must not retry a Location.
func (t *MarketplaceTransport) DialWebSocket(ctx context.Context, endpoint string, headers http.Header) (*websocket.Conn, *http.Response, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "wss" {
		return nil, nil, ErrMarketplaceUpstreamAddress
	}
	parsed.Scheme = "https"
	if ValidateMarketplaceURLFormat(parsed) != nil || (headers.Get("Host") != "" && headers.Get("Host") != parsed.Host) {
		return nil, nil, ErrMarketplaceUpstreamAddress
	}
	dialer := &websocket.Dialer{
		NetDialContext: t.DialContext, TLSClientConfig: t.TLSClientConfig,
		HandshakeTimeout: 20 * time.Second, EnableCompression: true,
	}
	return dialer.DialContext(ctx, endpoint, headers)
}

// DialMarketplaceWebSocket never uses environment proxies or insecure TLS.
func DialMarketplaceWebSocket(ctx context.Context, endpoint string, headers http.Header) (*websocket.Conn, *http.Response, error) {
	client := GetMarketplaceHTTPClientWithResponseHeaderTimeout(0)
	return client.Transport.(*MarketplaceTransport).DialWebSocket(ctx, endpoint, headers)
}
