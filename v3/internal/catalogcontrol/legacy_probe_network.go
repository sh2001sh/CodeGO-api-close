package catalogcontrol

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

const probeBodyLimit = 2 << 20
const probeTimeout = 15 * time.Second

var errProbeURL = errors.New("invalid upstream or proxy URL")

// Administrators may configure local Ollama and private upstreams. Metadata,
// unspecified and multicast addresses are never valid probe destinations.
func probeAddressAllowed(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && ip.String() != "255.255.255.255" && ip.String() != "fd00:ec2::254"
}

func validateProbeURL(raw string, proxy bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 4096 {
		return nil, errProbeURL
	}
	valid := u.Scheme == "http" || u.Scheme == "https"
	if proxy {
		valid = valid || u.Scheme == "socks5" || u.Scheme == "socks5h"
	}
	if !valid {
		return nil, errProbeURL
	}
	if ip, e := netip.ParseAddr(u.Hostname()); e == nil && !probeAddressAllowed(ip) {
		return nil, errProbeURL
	}
	return u, nil
}

func probeDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errProbeURL
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("upstream address could not be resolved")
	}
	for _, ip := range ips {
		if !probeAddressAllowed(ip) {
			return nil, errProbeURL
		}
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	for _, ip := range ips {
		conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if e == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errors.New("upstream connection failed")
}

func probeClient(proxyURL string) (*http.Client, func(), error) {
	if proxyURL != "" {
		if _, err := validateProbeURL(proxyURL, true); err != nil {
			return nil, nil, err
		}
	}
	pool := httpx.NewPool(httpx.TransportConfig{DialTimeout: 5 * time.Second, TLSHandshakeTimeout: 5 * time.Second})
	transport, err := pool.Transport(proxyURL)
	if err != nil {
		return nil, nil, errProbeURL
	}
	transport.DialContext = probeDial
	transport.ResponseHeaderTimeout = 10 * time.Second
	transport.MaxResponseHeaderBytes = 64 << 10
	client := &http.Client{Transport: transport, Timeout: probeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, pool.CloseIdle, nil
}
