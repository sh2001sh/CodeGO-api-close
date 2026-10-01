package credentials

import (
	"context"
	"net/http"
	"net/netip"
	"strings"

	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

var _ httpx.PinnedMarketTransport = (*identityTransport)(nil)

// PublicMarketTransport isolates a marketplace connection at the validated
// public origin without changing the native provider's request or credentials.
func (t *identityTransport) PublicMarketTransport(host string, address netip.Addr) (http.RoundTripper, error) {
	if t == nil || t.transport == nil {
		return nil, httpx.ErrMarketTransportPolicy
	}
	dial, err := imageProxyDialer(t.imageProxy, t.imageConfig.withDefaults())
	if err != nil {
		return nil, err
	}
	return newMarketTransport(host, address, t.imageConfig, t.imageFingerprint, dial)
}

func newMarketTransport(host string, address netip.Addr, cfg TransportConfig, fp Fingerprint, dial dialContext) (*marketRelayTransport, error) {
	pinned, err := newImageTransport(host, address, cfg, fp, dial)
	if err != nil {
		return nil, httpx.ErrMarketTransportPolicy
	}
	return &marketRelayTransport{transport: pinned.transport, host: pinned.host, userAgent: pinned.userAgent}, nil
}

type marketRelayTransport struct {
	transport       *http.Transport
	host, userAgent string
}

func (t *marketRelayTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil || t.transport == nil || req == nil || req.URL == nil || req.URL.Scheme != "https" ||
		!strings.EqualFold(req.URL.Hostname(), t.host) || req.URL.User != nil || req.URL.Fragment != "" ||
		strings.Contains(req.URL.Host, "%") ||
		(req.URL.Port() != "" && req.URL.Port() != "443" && req.URL.Port() != "8443") ||
		(req.Host != "" && !strings.EqualFold(req.Host, req.URL.Host)) {
		return nil, httpx.ErrMarketTransportPolicy
	}
	clone := req.Clone(context.WithValue(req.Context(), imageRequestContextKey{}, req.Context()))
	clone.Host = req.URL.Host
	if clone.Header == nil {
		clone.Header = make(http.Header)
	}
	clone.Header.Set("User-Agent", t.userAgent)
	// Proxy authentication belongs only on the configured CONNECT tunnel.
	clone.Header.Del("Proxy-Authorization")
	return t.transport.RoundTrip(clone)
}

func (t *marketRelayTransport) CloseIdleConnections() {
	if t != nil && t.transport != nil {
		t.transport.CloseIdleConnections()
	}
}
