package adminops

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

func toolAddressAllowed(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && ip.String() != "255.255.255.255" && ip.String() != "fd00:ec2::254"
}
func validateToolURL(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 4096 || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("invalid upstream URL")
	}
	if ip, e := netip.ParseAddr(u.Hostname()); e == nil && !toolAddressAllowed(ip) {
		return nil, errors.New("invalid upstream address")
	}
	return u, nil
}
func toolDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("invalid upstream address")
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("upstream DNS resolution failed")
	}
	for _, ip := range ips {
		if !toolAddressAllowed(ip) {
			return nil, errors.New("upstream address is forbidden")
		}
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	for _, ip := range ips {
		conn, e := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if e == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errors.New("upstream connection failed")
}
func defaultToolClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{DialContext: toolDial, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxResponseHeaderBytes: 64 << 10, MaxIdleConns: 32, IdleConnTimeout: time.Minute}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
