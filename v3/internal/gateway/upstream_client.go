package gateway

import (
	"context"
	"errors"
	"net/http"
)

// OverrideProviderTransport preserves native exchanges through wrappers without
// replacing the channel's credential identity or proxy for ordinary HTTP calls.
func OverrideProviderTransport(provider Provider, req *Request, fallback http.RoundTripper) http.RoundTripper {
	if selected, ok := provider.(TransportProvider); ok {
		return selected.UpstreamTransport(req, fallback)
	}
	if native, ok := provider.(http.RoundTripper); ok {
		return native
	}
	return fallback
}

func (g *Gateway) upstreamClient(ctx context.Context, target Target) (*http.Client, error) {
	if g.clients != nil {
		client, err := g.clients(ctx, target)
		if err != nil {
			return nil, err
		}
		if client == nil {
			return nil, errors.New("gateway: configured upstream client is unavailable")
		}
		copy := *client
		if copy.Transport == nil {
			copy.Transport = http.DefaultTransport
		}
		if copy.CheckRedirect == nil {
			copy.CheckRedirect = rejectUpstreamRedirect
		}
		return &copy, nil
	}
	transport, err := g.transports.Transport(target.ProxyURL)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: transport, CheckRedirect: rejectUpstreamRedirect}, nil
}

func rejectUpstreamRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
