package credentials

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

type dialContext func(context.Context, string, string) (net.Conn, error)

// proxyDialer tunnels before performing the upstream uTLS handshake. Relying on
// http.Transport.Proxy would bypass DialTLSContext after CONNECT and silently
// replace the configured fingerprint with Go's standard TLS client.
// socks5Dialer builds a SOCKS5 dialContext for the given proxy URL.
func socks5Dialer(u *url.URL, dialer *net.Dialer) (dialContext, error) {
	var auth *proxy.Auth
	if u.User != nil {
		password, _ := u.User.Password()
		auth = &proxy.Auth{User: u.User.Username(), Password: password}
	}
	addr := u.Host
	if u.Port() == "" {
		addr = net.JoinHostPort(u.Hostname(), "1080")
	}
	socks, err := proxy.SOCKS5("tcp", addr, auth, dialer)
	if err != nil {
		return nil, errors.New("credentials: invalid SOCKS proxy")
	}
	contextDialer, ok := socks.(proxy.ContextDialer)
	if !ok {
		return nil, errors.New("credentials: SOCKS proxy does not support cancellation")
	}
	return contextDialer.DialContext, nil
}

// httpConnectTunnel dials addr through dialer, optionally wraps it in TLS for
// an https proxy, and performs the HTTP CONNECT handshake to target. The
// returned conn has its deadline cleared and is ready for the caller's own
// use (e.g. a further TLS handshake to the real upstream).
func httpConnectTunnel(tunnel context.Context, dialer *net.Dialer, u *url.URL, addr, network, target string) (net.Conn, error) {
	conn, err := dialer.DialContext(tunnel, network, addr)
	if err != nil {
		return nil, errors.New("credentials: proxy connection failed")
	}
	rawConn := conn
	stop := context.AfterFunc(tunnel, func() { _ = rawConn.Close() })
	defer stop()
	if deadline, ok := tunnel.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if u.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
		if err = tlsConn.HandshakeContext(tunnel); err != nil {
			_ = conn.Close()
			return nil, errors.New("credentials: proxy TLS failed")
		}
		conn = tlsConn
	}
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: target}, Host: target, Header: make(http.Header)}
	if u.User != nil {
		password, _ := u.User.Password()
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+password)))
	}
	if err = req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, errors.New("credentials: proxy CONNECT failed")
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		_ = conn.Close()
		return nil, errors.New("credentials: invalid proxy response")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		_ = conn.Close()
		return nil, errors.New("credentials: proxy rejected CONNECT")
	}
	if reader.Buffered() != 0 {
		_ = conn.Close()
		return nil, errors.New("credentials: proxy sent unexpected tunnel data")
	}
	if !stop() && tunnel.Err() != nil {
		_ = conn.Close()
		return nil, tunnel.Err()
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// httpProxyDialer builds a dialContext that tunnels through an HTTP(S) proxy
// via CONNECT, enforcing timeout as the deadline for the proxy connection and
// handshake.
func httpProxyDialer(u *url.URL, dialer *net.Dialer, timeout time.Duration) dialContext {
	addr := u.Host
	if u.Port() == "" {
		port := "80"
		if u.Scheme == "https" {
			port = "443"
		}
		addr = net.JoinHostPort(u.Hostname(), port)
	}
	return func(ctx context.Context, network, target string) (net.Conn, error) {
		tunnel, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return httpConnectTunnel(tunnel, dialer, u, addr, network, target)
	}
}

// proxyDialer tunnels before performing the upstream uTLS handshake. Relying on
// http.Transport.Proxy would bypass DialTLSContext after CONNECT and silently
// replace the configured fingerprint with Go's standard TLS client.
func proxyDialer(raw string, timeout time.Duration) (dialContext, error) {
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	if raw == "" {
		return dialer.DialContext, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, errors.New("credentials: invalid proxy URL")
	}
	switch u.Scheme {
	case "socks5", "socks5h":
		return socks5Dialer(u, dialer)
	case "http", "https":
		return httpProxyDialer(u, dialer, timeout), nil
	default:
		return nil, errors.New("credentials: unsupported proxy scheme")
	}
}
