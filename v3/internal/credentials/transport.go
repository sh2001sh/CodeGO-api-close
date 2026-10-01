package credentials

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
)

type TransportConfig struct {
	RootCAs             *x509.CertPool
	DialTimeout         time.Duration
	TLSHandshakeTimeout time.Duration
	IdleTimeout         time.Duration
	MaxIdleConnsPerHost int
}

func (c TransportConfig) withDefaults() TransportConfig {
	if c.DialTimeout == 0 {
		c.DialTimeout = 10 * time.Second
	}
	if c.TLSHandshakeTimeout == 0 {
		c.TLSHandshakeTimeout = 10 * time.Second
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = 90 * time.Second
	}
	if c.MaxIdleConnsPerHost == 0 {
		c.MaxIdleConnsPerHost = 32
	}
	return c
}

type clientKey struct {
	id                 int64
	proxy, ua, profile string
}
type TransportPool struct {
	cfg     TransportConfig
	mu      sync.Mutex
	clients map[clientKey]*http.Client
}

func NewTransportPool(cfg TransportConfig) *TransportPool {
	return &TransportPool{cfg: cfg.withDefaults(), clients: make(map[clientKey]*http.Client)}
}

func defaultFingerprint(fp Fingerprint) Fingerprint {
	if fp.TLSProfile == "" {
		fp.TLSProfile = "chrome"
	}
	if fp.UserAgent == "" {
		if fp.TLSProfile == "firefox" {
			fp.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0"
		} else {
			fp.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
		}
	}
	return fp
}

// uTLSHandshakeConn completes a uTLS handshake over conn, impersonating fp's
// TLS profile and negotiating HTTP/1.1 only (net/http's HTTP/2 integration
// requires a crypto/tls.Conn and cannot inspect a uTLS.UConn's TLS state).
func uTLSHandshakeConn(ctx context.Context, cfg TransportConfig, fp Fingerprint, conn net.Conn, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	hello := utls.HelloChrome_120
	if fp.TLSProfile == "firefox" {
		hello = utls.HelloFirefox_120
	}
	spec, err := utls.UTLSIdToSpec(hello)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	for i, ext := range spec.Extensions {
		if _, ok := ext.(*utls.ALPNExtension); ok {
			spec.Extensions[i] = &utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}}
		}
		if _, ok := ext.(*utls.ApplicationSettingsExtension); ok {
			spec.Extensions[i] = &utls.ApplicationSettingsExtension{SupportedProtocols: []string{"http/1.1"}}
		}
	}
	tlsConn := utls.UClient(conn, &utls.Config{ServerName: host, RootCAs: cfg.RootCAs}, utls.HelloCustom)
	if err = tlsConn.ApplyPreset(&spec); err == nil {
		handshake, cancel := context.WithTimeout(ctx, cfg.TLSHandshakeTimeout)
		err = tlsConn.HandshakeContext(handshake)
		cancel()
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

// buildFingerprintedTransport builds the *http.Transport that dials through
// dial and performs a uTLS handshake impersonating fp for every TLS
// connection.
func (p *TransportPool) buildFingerprintedTransport(dial dialContext, fp Fingerprint) *http.Transport {
	transport := &http.Transport{
		DialContext: dial, MaxIdleConns: 256, MaxIdleConnsPerHost: p.cfg.MaxIdleConnsPerHost,
		IdleConnTimeout: p.cfg.IdleTimeout, TLSHandshakeTimeout: p.cfg.TLSHandshakeTimeout,
		ExpectContinueTimeout: time.Second, ForceAttemptHTTP2: false,
	}
	transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return uTLSHandshakeConn(ctx, p.cfg, fp, conn, addr)
	}
	return transport
}

// evictCredentialClients closes and removes every pooled client for id other
// than key, so a changed proxy/profile cannot accumulate idle connections
// forever under the old pool.
func (p *TransportPool) evictCredentialClients(id int64) {
	for old, c := range p.clients {
		if old.id == id {
			c.CloseIdleConnections()
			delete(p.clients, old)
		}
	}
}

// Client caches a connection pool and immutable UA/TLS identity per credential.
// Requests share idle uTLS connections instead of handshaking for every call.
// HTTP/1.1 is explicitly negotiated because net/http's HTTP/2 integration
// requires a crypto/tls.Conn and cannot inspect a uTLS.UConn's TLS state.
func (p *TransportPool) Client(id int64, proxyURL string, fp Fingerprint) (*http.Client, Fingerprint, error) {
	fp = defaultFingerprint(fp)
	if fp.TLSProfile != "chrome" && fp.TLSProfile != "firefox" {
		return nil, fp, errors.New("credentials: unsupported TLS profile")
	}
	if err := validateUserAgent(fp.UserAgent); err != nil {
		return nil, fp, err
	}
	key := clientKey{id: id, proxy: proxyURL, ua: fp.UserAgent, profile: fp.TLSProfile}
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.clients[key]; c != nil {
		return c, fp, nil
	}
	dial, err := proxyDialer(proxyURL, p.cfg.DialTimeout)
	if err != nil {
		return nil, fp, err
	}
	transport := p.buildFingerprintedTransport(dial, fp)
	client := &http.Client{Transport: &identityTransport{transport: transport, userAgent: fp.UserAgent,
		imageConfig: p.cfg, imageFingerprint: fp, imageProxy: proxyURL}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	p.evictCredentialClients(id)
	p.clients[key] = client
	return client, fp, nil
}

func (p *TransportPool) CloseIdle() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.clients {
		c.CloseIdleConnections()
	}
}

type identityTransport struct {
	transport        *http.Transport
	userAgent        string
	imageConfig      TransportConfig
	imageFingerprint Fingerprint
	imageProxy       string
}

func (t *identityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("User-Agent", t.userAgent)
	return t.transport.RoundTrip(clone)
}
func (t *identityTransport) CloseIdleConnections() { t.transport.CloseIdleConnections() }

func validateUserAgent(ua string) error {
	if len(ua) > 1024 {
		return errors.New("credentials: user agent too long")
	}
	for _, r := range ua {
		if r < ' ' || r == 127 {
			return errors.New("credentials: invalid user agent")
		}
	}
	return nil
}
