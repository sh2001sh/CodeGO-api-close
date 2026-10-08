// Package httpx builds pooled HTTP transports for upstream providers.
package httpx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// TransportConfig tunes connection pooling. Zero values select defaults.
type TransportConfig struct {
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	TLSHandshakeTimeout time.Duration
	DialTimeout         time.Duration
}

func (c TransportConfig) withDefaults() TransportConfig {
	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = 2048
	}
	if c.MaxIdleConnsPerHost == 0 {
		c.MaxIdleConnsPerHost = 512
	}
	if c.IdleConnTimeout == 0 {
		c.IdleConnTimeout = 90 * time.Second
	}
	if c.TLSHandshakeTimeout == 0 {
		c.TLSHandshakeTimeout = 10 * time.Second
	}
	if c.DialTimeout == 0 {
		c.DialTimeout = 10 * time.Second
	}
	return c
}

// Pool hands out one shared transport per proxy URL. v2 created a transport
// per response-header-timeout bucket, which split connection pools; here the
// header timeout is enforced per request with WithHeaderTimeout instead.
type Pool struct {
	cfg           TransportConfig
	mu            sync.Mutex
	transports    map[string]*http.Transport
	marketClients map[marketClientKey]*marketClientEntry
	marketClock   uint64
}

// NewPool returns an empty transport pool.
func NewPool(cfg TransportConfig) *Pool {
	return &Pool{cfg: cfg.withDefaults(), transports: make(map[string]*http.Transport)}
}

// Transport returns the shared transport for proxyURL ("" means direct).
func (p *Pool) Transport(proxyURL string) (*http.Transport, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok := p.transports[proxyURL]; ok {
		return t, nil
	}
	t, err := p.newTransport(proxyURL)
	if err != nil {
		return nil, err
	}
	p.transports[proxyURL] = t
	return t, nil
}

func (p *Pool) newTransport(proxyURL string) (*http.Transport, error) {
	dialer := &net.Dialer{Timeout: p.cfg.DialTimeout, KeepAlive: 30 * time.Second}
	t := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          p.cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   p.cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:       p.cfg.IdleConnTimeout,
		TLSHandshakeTimeout:   p.cfg.TLSHandshakeTimeout,
		ExpectContinueTimeout: time.Second,
	}
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		switch u.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return nil, errors.New("httpx: unsupported proxy scheme " + u.Scheme)
		}
		t.Proxy = http.ProxyURL(u)
	}
	return t, nil
}

// CloseIdle closes idle connections on every pooled transport.
func (p *Pool) CloseIdle() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range p.transports {
		t.CloseIdleConnections()
	}
	for _, cached := range p.marketClients {
		cached.transport.CloseIdleConnections()
	}
}

// ErrHeaderTimeout is the cancellation cause when upstream headers are late.
var ErrHeaderTimeout = errors.New("httpx: upstream response header timeout")

// WithHeaderTimeout derives a context that is cancelled with ErrHeaderTimeout
// unless the returned stop function is called (once headers arrive) within d.
// The body keeps streaming after stop; the parent context still applies.
func WithHeaderTimeout(parent context.Context, d time.Duration) (ctx context.Context, stop func(), cancel context.CancelFunc) {
	ctx, cancelCause := context.WithCancelCause(parent)
	timer := time.AfterFunc(d, func() { cancelCause(ErrHeaderTimeout) })
	stop = func() { timer.Stop() }
	cancel = func() {
		timer.Stop()
		cancelCause(context.Canceled)
	}
	return ctx, stop, cancel
}
