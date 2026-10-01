package credentials

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"net"
	"net/http"
	"net/url"
	"time"
)

// imageProxyConnect dials proxyAddress through dialer, optionally wraps it in
// TLS for an https proxy (validated against cfg.RootCAs), and performs the
// HTTP CONNECT handshake to target. The returned conn has its deadline
// cleared and is ready for the caller's own use.
func imageProxyConnect(tunnel context.Context, dialer *net.Dialer, u *url.URL, cfg TransportConfig, proxyAddress, network, target string) (net.Conn, error) {
	conn, err := dialer.DialContext(tunnel, network, proxyAddress)
	if err != nil {
		return nil, errImageProxy
	}
	underlying := conn
	stop := context.AfterFunc(tunnel, func() { _ = underlying.Close() })
	defer stop()
	if deadline, ok := tunnel.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if u.Scheme == "https" {
		secured := tls.Client(conn, &tls.Config{ServerName: u.Hostname(), RootCAs: cfg.RootCAs, MinVersion: tls.VersionTLS12})
		if err = secured.HandshakeContext(tunnel); err != nil {
			_ = conn.Close()
			return nil, errImageProxy
		}
		conn = secured
	}
	connect := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: target}, Host: target, Header: make(http.Header)}
	if u.User != nil {
		password, _ := u.User.Password()
		connect.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+password)))
	}
	if err = connect.Write(conn); err != nil {
		_ = conn.Close()
		return nil, errImageProxy
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, connect)
	if err != nil {
		_ = conn.Close()
		return nil, errImageProxy
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		_ = conn.Close()
		return nil, errImageProxy
	}
	if reader.Buffered() != 0 {
		_ = conn.Close()
		return nil, errImageProxy
	}
	if !stop() && tunnel.Err() != nil {
		_ = conn.Close()
		return nil, tunnel.Err()
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// CONNECT always names the already-pinned origin IP. The configured proxy may
// resolve its own host, but cannot resolve the image's DNS name a second time.
func imageProxyDialer(raw string, cfg TransportConfig) (dialContext, error) {
	if raw == "" {
		return proxyDialer("", cfg.DialTimeout)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, errImageProxy
	}
	if u.Scheme == "socks5" || u.Scheme == "socks5h" {
		return proxyDialer(raw, cfg.DialTimeout)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errImageProxy
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	proxyAddress := net.JoinHostPort(u.Hostname(), port)
	dialer := &net.Dialer{Timeout: cfg.DialTimeout, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, target string) (net.Conn, error) {
		tunnel, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
		defer cancel()
		return imageProxyConnect(tunnel, dialer, u, cfg, proxyAddress, network, target)
	}, nil
}
