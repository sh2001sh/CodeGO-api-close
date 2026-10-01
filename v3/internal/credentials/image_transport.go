package credentials

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

var _ httpx.PinnedImageTransport = (*identityTransport)(nil)

// PublicImageTransport preserves this credential's network identity, but its
// origin address and headers are isolated from the provider connection pool.
func (t *identityTransport) PublicImageTransport(host string, address netip.Addr) (http.RoundTripper, error) {
	if t == nil || t.transport == nil {
		return nil, httpx.ErrImageTransportPolicy
	}
	dial, err := imageProxyDialer(t.imageProxy, t.imageConfig.withDefaults())
	if err != nil {
		return nil, err
	}
	return newImageTransport(host, address, t.imageConfig, t.imageFingerprint, dial)
}

// The explicit dial argument permits local network fixtures without adding a
// production bypass for public-address validation or certificate verification.
func newImageTransport(host string, address netip.Addr, cfg TransportConfig, fp Fingerprint, dial dialContext) (*imageTransport, error) {
	address = address.Unmap()
	if !safeImageAddress(address) || host == "" || strings.ContainsAny(host, "/\\?#@% \t\r\n") || strings.ContainsRune(host, 0) || dial == nil {
		return nil, httpx.ErrImageTransportPolicy
	}
	if strings.Contains(host, ":") {
		if _, err := netip.ParseAddr(host); err != nil {
			return nil, httpx.ErrImageTransportPolicy
		}
	}
	cfg = cfg.withDefaults()
	fp = defaultFingerprint(fp)
	if (fp.TLSProfile != "chrome" && fp.TLSProfile != "firefox") || validateUserAgent(fp.UserAgent) != nil {
		return nil, httpx.ErrImageTransportPolicy
	}
	pinnedDial := func(ctx context.Context, network, authority string) (net.Conn, error) {
		requested, port, err := net.SplitHostPort(authority)
		if err != nil || !strings.EqualFold(requested, host) {
			return nil, httpx.ErrImageTransportPolicy
		}
		return dial(ctx, network, net.JoinHostPort(address.String(), port))
	}
	transport := &http.Transport{MaxIdleConns: 4, MaxIdleConnsPerHost: 2,
		IdleConnTimeout: cfg.IdleTimeout, TLSHandshakeTimeout: cfg.TLSHandshakeTimeout, ExpectContinueTimeout: time.Second, ForceAttemptHTTP2: false}
	transport.DialContext = func(ctx context.Context, network, authority string) (net.Conn, error) {
		ctx, cancel := imageDialContext(ctx)
		defer cancel()
		return pinnedDial(ctx, network, authority)
	}
	transport.DialTLSContext = func(ctx context.Context, network, authority string) (net.Conn, error) {
		ctx, cancel := imageDialContext(ctx)
		defer cancel()
		conn, err := pinnedDial(ctx, network, authority)
		if err != nil {
			return nil, err
		}
		return imageTLSConnection(ctx, conn, host, cfg, fp)
	}
	return &imageTransport{transport: transport, host: host, userAgent: fp.UserAgent}, nil
}

type imageTransport struct {
	transport       *http.Transport
	host, userAgent string
}

type imageRequestContextKey struct{}

// net/http detaches dialing from request cancellation so a connection can serve
// another request. Image connections must stop when their fetch is cancelled.
func imageDialContext(ctx context.Context) (context.Context, context.CancelFunc) {
	request, ok := ctx.Value(imageRequestContextKey{}).(context.Context)
	if !ok {
		return context.WithCancel(ctx)
	}
	bound, cancel := context.WithCancel(request)
	stop := context.AfterFunc(ctx, cancel)
	return bound, func() {
		stop()
		cancel()
	}
}

func (t *imageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil || t.transport == nil || req == nil || req.URL == nil || req.Method != http.MethodGet ||
		(req.Body != nil && req.Body != http.NoBody) || req.URL.User != nil || req.URL.Fragment != "" ||
		(req.URL.Scheme != "http" && req.URL.Scheme != "https") || !strings.EqualFold(req.URL.Hostname(), t.host) {
		return nil, httpx.ErrImageTransportPolicy
	}
	clone := req.Clone(context.WithValue(req.Context(), imageRequestContextKey{}, req.Context()))
	clone.Host = req.URL.Host
	// Copy only image negotiation. Auth, cookies, proxy auth, API keys, request
	// trailers and any provider-specific headers cannot cross this boundary.
	clone.Header = make(http.Header)
	for _, accept := range req.Header.Values("Accept") {
		clone.Header.Add("Accept", accept)
	}
	clone.Header.Set("User-Agent", t.userAgent)
	clone.Trailer = nil
	clone.TransferEncoding = nil
	clone.ContentLength = 0
	return t.transport.RoundTrip(clone)
}

func (t *imageTransport) CloseIdleConnections() {
	if t != nil && t.transport != nil {
		t.transport.CloseIdleConnections()
	}
}

func safeImageAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "100::/64", "2001::/32", "2001:db8::/32", "2002::/16"} {
		if netip.MustParsePrefix(raw).Contains(address) {
			return false
		}
	}
	return true
}

var errImageProxy = errors.New("credentials: image proxy connection failed")
