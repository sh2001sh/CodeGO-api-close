package httpx

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	"golang.org/x/net/proxy"
)

var (
	httpClient                    *http.Client
	proxyClientLock               sync.Mutex
	proxyClients                  = make(map[string]*http.Client)
	responseHeaderTimeoutClients  = make(map[time.Duration]*http.Client)
	responseHeaderTimeoutClientMu sync.Mutex
)

const outboundConnectionTimeout = 10 * time.Second

func relayResponseHeaderTimeout() time.Duration {
	if platformconfig.RelayResponseHeaderTimeout <= 0 {
		return 0
	}
	return time.Duration(platformconfig.RelayResponseHeaderTimeout) * time.Second
}

func newOutboundTransport(proxyFunc func(*http.Request) (*url.URL, error), dialContext func(context.Context, string, string) (net.Conn, error)) *http.Transport {
	return newOutboundTransportWithResponseHeaderTimeout(proxyFunc, dialContext, relayResponseHeaderTimeout())
}

func newOutboundTransportWithResponseHeaderTimeout(proxyFunc func(*http.Request) (*url.URL, error), dialContext func(context.Context, string, string) (net.Conn, error), responseHeaderTimeout time.Duration) *http.Transport {
	idleConnTimeout := time.Duration(platformconfig.RelayIdleConnTimeoutSeconds) * time.Second
	if idleConnTimeout <= 0 {
		idleConnTimeout = 90 * time.Second
	}
	tlsHandshakeTimeout := time.Duration(platformconfig.RelayTLSHandshakeTimeoutSeconds) * time.Second
	if tlsHandshakeTimeout <= 0 {
		tlsHandshakeTimeout = outboundConnectionTimeout
	}
	dialer := &net.Dialer{Timeout: outboundConnectionTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		MaxIdleConns:          platformconfig.RelayMaxIdleConns,
		MaxIdleConnsPerHost:   platformconfig.RelayMaxIdleConnsPerHost,
		MaxConnsPerHost:       platformconfig.RelayMaxConnsPerHost,
		IdleConnTimeout:       idleConnTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		Proxy:                 proxyFunc,
		DialContext:           dialContext,
	}
	if responseHeaderTimeout > 0 {
		transport.ResponseHeaderTimeout = responseHeaderTimeout
	}
	// Do not couple connection establishment to the optional response-header
	// budget. Disabling the latter must not permit stalled TCP/TLS handshakes.
	transport.TLSHandshakeTimeout = tlsHandshakeTimeout
	if transport.DialContext == nil {
		transport.DialContext = dialer.DialContext
	}
	if platformconfig.TLSInsecureSkipVerify {
		transport.TLSClientConfig = platformconfig.InsecureTLSConfig
	}
	return transport
}

func newOutboundHTTPClient(transport *http.Transport) *http.Client {
	client := &http.Client{Transport: transport, CheckRedirect: checkRedirect}
	if platformconfig.RelayTimeout > 0 {
		client.Timeout = time.Duration(platformconfig.RelayTimeout) * time.Second
	}
	return client
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	fetchSetting := platformstore.GetFetchSetting()
	urlStr := req.URL.String()
	if err := platformsecurity.ValidateURLWithFetchSetting(urlStr, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return fmt.Errorf("redirect to %s blocked: %v", urlStr, err)
	}
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	return nil
}

// InitHTTPClient initializes the shared outbound HTTP client.
func InitHTTPClient() {
	responseHeaderTimeoutClientMu.Lock()
	responseHeaderTimeoutClients = make(map[time.Duration]*http.Client)
	responseHeaderTimeoutClientMu.Unlock()
	httpClient = newOutboundHTTPClient(newOutboundTransport(http.ProxyFromEnvironment, nil))
}

// GetHTTPClient returns the shared outbound HTTP client.
func GetHTTPClient() *http.Client {
	return httpClient
}

func sharedHTTPClientOrDefault() *http.Client {
	if client := GetHTTPClient(); client != nil {
		return client
	}
	return http.DefaultClient
}

// GetHTTPClientWithResponseHeaderTimeout returns a shared client for a
// request-specific first-byte budget. The global client remains unchanged.
func GetHTTPClientWithResponseHeaderTimeout(responseHeaderTimeout time.Duration) *http.Client {
	// The request-specific timeout may be lower than the global relay budget
	// (for example, the 20s GPT first-byte budget). Reuse the global client
	// only when the budgets are identical; otherwise its transport would
	// silently restore the global timeout.
	if responseHeaderTimeout <= 0 || responseHeaderTimeout == relayResponseHeaderTimeout() {
		return sharedHTTPClientOrDefault()
	}
	responseHeaderTimeoutClientMu.Lock()
	defer responseHeaderTimeoutClientMu.Unlock()
	if client := responseHeaderTimeoutClients[responseHeaderTimeout]; client != nil {
		return client
	}
	client := newOutboundHTTPClient(newOutboundTransportWithResponseHeaderTimeout(http.ProxyFromEnvironment, nil, responseHeaderTimeout))
	responseHeaderTimeoutClients[responseHeaderTimeout] = client
	return client
}

// GetHTTPClientWithProxy returns the shared client or a proxy-enabled client.
func GetHTTPClientWithProxy(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return sharedHTTPClientOrDefault(), nil
	}
	return NewProxyHTTPClient(proxyURL)
}

// ResetProxyClientCache clears cached proxy-specific HTTP clients.
func ResetProxyClientCache() {
	proxyClientLock.Lock()
	defer proxyClientLock.Unlock()
	for _, client := range proxyClients {
		if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
			transport.CloseIdleConnections()
		}
	}
	proxyClients = make(map[string]*http.Client)
	responseHeaderTimeoutClientMu.Lock()
	for _, client := range responseHeaderTimeoutClients {
		if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
			transport.CloseIdleConnections()
		}
	}
	responseHeaderTimeoutClients = make(map[time.Duration]*http.Client)
	responseHeaderTimeoutClientMu.Unlock()
}

// NewProxyHTTPClient creates or reuses a proxy-specific HTTP client.
func NewProxyHTTPClient(proxyURL string) (*http.Client, error) {
	return newProxyHTTPClient(proxyURL, relayResponseHeaderTimeout())
}

// NewProxyHTTPClientWithResponseHeaderTimeout applies a request-specific
// first-byte budget while retaining proxy connection reuse.
func NewProxyHTTPClientWithResponseHeaderTimeout(proxyURL string, responseHeaderTimeout time.Duration) (*http.Client, error) {
	if responseHeaderTimeout <= 0 || responseHeaderTimeout == relayResponseHeaderTimeout() {
		return NewProxyHTTPClient(proxyURL)
	}
	return newProxyHTTPClient(proxyURL, responseHeaderTimeout)
}

func newProxyHTTPClient(proxyURL string, responseHeaderTimeout time.Duration) (*http.Client, error) {
	if proxyURL == "" {
		return GetHTTPClientWithResponseHeaderTimeout(responseHeaderTimeout), nil
	}

	cacheKey := proxyURL + "\x00" + responseHeaderTimeout.String()
	proxyClientLock.Lock()
	if client, ok := proxyClients[cacheKey]; ok {
		proxyClientLock.Unlock()
		return client, nil
	}
	proxyClientLock.Unlock()

	parsedURL, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}

	switch parsedURL.Scheme {
	case "http", "https":
		transport := newOutboundTransportWithResponseHeaderTimeout(http.ProxyURL(parsedURL), nil, responseHeaderTimeout)
		client := newOutboundHTTPClient(transport)
		proxyClientLock.Lock()
		proxyClients[cacheKey] = client
		proxyClientLock.Unlock()
		return client, nil

	case "socks5", "socks5h":
		var auth *proxy.Auth
		if parsedURL.User != nil {
			auth = &proxy.Auth{
				User:     parsedURL.User.Username(),
				Password: "",
			}
			if password, ok := parsedURL.User.Password(); ok {
				auth.Password = password
			}
		}

		dialer, err := proxy.SOCKS5("tcp", parsedURL.Host, auth, proxy.Direct)
		if err != nil {
			return nil, err
		}

		transport := newOutboundTransportWithResponseHeaderTimeout(nil, func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}, responseHeaderTimeout)

		client := newOutboundHTTPClient(transport)
		proxyClientLock.Lock()
		proxyClients[cacheKey] = client
		proxyClientLock.Unlock()
		return client, nil

	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s, must be http, https, socks5 or socks5h", parsedURL.Scheme)
	}
}
