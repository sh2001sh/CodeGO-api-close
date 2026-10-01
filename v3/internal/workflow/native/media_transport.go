package native

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"

	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

// Public media uses the same credential identity through the pool's pinned GET
// transport. Pinning preserves proxy/TLS/UA without letting a result URL select
// a private DNS address or receive upstream credentials.
func publicMediaTransport(ctx context.Context, u *url.URL) (http.RoundTripper, func(), error) {
	noop := func() {}
	policy, _ := ctx.Value(requestContextKey{}).(requestPolicy)
	if policy.clients == nil {
		return resultTransport, noop, nil
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", u.Hostname())
	if err != nil || len(ips) == 0 {
		return nil, noop, errors.New("task result address unavailable")
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, noop, errors.New("private task result address")
		}
	}
	client, err := policy.clients(ctx, policy.target)
	if err != nil || client == nil {
		return nil, noop, errors.New("task credential transport unavailable")
	}
	pinned, ok := client.Transport.(httpx.PinnedImageTransport)
	if !ok {
		return nil, noop, errors.New("task credential transport cannot pin public media")
	}
	transport, err := pinned.PublicImageTransport(u.Hostname(), ips[0].Unmap())
	if err != nil || transport == nil {
		return nil, noop, errors.New("task result pinned transport unavailable")
	}
	if idle, ok := transport.(interface{ CloseIdleConnections() }); ok {
		return transport, idle.CloseIdleConnections, nil
	}
	return transport, noop, nil
}
