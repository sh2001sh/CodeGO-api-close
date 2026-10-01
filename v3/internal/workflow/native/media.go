package native

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// A provider result URL is untrusted. Resolve and validate at dial time so a
// redirect or DNS rebinding cannot reach internal services. The configured
// upstream endpoint itself remains an explicitly trusted administrator target.
func Media(ctx context.Context, c *http.Client, target gateway.Target, raw string, headers http.Header) (*http.Response, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return nil, errors.New("invalid task result URL")
	}
	base, _ := url.Parse(target.BaseURL)
	trusted := base != nil && base.Host == u.Host && base.Scheme == u.Scheme
	if u.Scheme != "https" && (!trusted || u.Scheme != "http") {
		return nil, errors.New("unsafe task result URL")
	}
	client := Client(c)
	if !trusted {
		transport, closeIdle, err := publicMediaTransport(ctx, u)
		if err != nil {
			return nil, err
		}
		defer closeIdle()
		client.Transport = transport
	} else {
		ctx = WithTarget(ctx, target)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if headers != nil && trusted {
		req.Header = headers.Clone()
	}
	var resp *http.Response
	if trusted {
		resp, err = Do(client, req)
	} else {
		// External result URLs receive no credential headers or channel overrides;
		// a selected identity uses a pinned transport with the same SSRF boundary.
		resp, err = client.Do(req)
		if err == nil {
			resp.StatusCode = gateway.MapUpstreamStatus(resp.StatusCode, target)
		}
	}
	if err != nil {
		return nil, errors.New("task result download failed")
	}
	return resp, nil
}

var resultTransport = &http.Transport{
	ForceAttemptHTTP2: true, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second,
	DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("private task result address")
			}
		}
		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		var last error
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		if last == nil {
			last = errors.New("no task result addresses")
		}
		return nil, last
	},
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "100::/64", "2001::/32", "2001:db8::/32", "2002::/16"} {
		if netip.MustParsePrefix(raw).Contains(ip) {
			return false
		}
	}
	return true
}
