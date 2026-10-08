package httpx

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"
)

var ErrMarketTransportPolicy = errors.New("httpx: marketplace requires a public pinned upstream")

// PublicUpstreamAddress shares the same policy for market relay and verification.
func PublicUpstreamAddress(address netip.Addr) bool {
	return address.Zone() == "" && publicImageAddress(address)
}

// PinnedMarketTransport preserves configured TLS fingerprint and proxy while
// dialing only address and validating the origin certificate against host.
// Unlike image transports, it must preserve the relay method, body and headers.
type PinnedMarketTransport interface {
	PublicMarketTransport(host string, address netip.Addr) (http.RoundTripper, error)
}

// MarketClient applies marketplace URL/DNS policy to the selected client. The
// original client remains usable for ordinary channels with its own policy.
func MarketClient(selected *http.Client) (*http.Client, error) {
	if selected == nil {
		return nil, ErrMarketTransportPolicy
	}
	base := selected.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if _, custom := base.(PinnedMarketTransport); !custom {
		if _, standard := base.(*http.Transport); !standard {
			return nil, ErrMarketTransportPolicy
		}
	}
	client := *selected
	client.Transport = &marketTransport{base: base, lookup: net.DefaultResolver.LookupNetIP}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client, nil
}

type marketClientKey struct {
	credentialID int64
	base         http.RoundTripper
}

type marketClientEntry struct {
	transport *marketTransport
	used      uint64
}

const marketClientsPerPool = 256

// MarketClient retains marketplace origin pools across target selection. The
// upstream credential identity is part of the key even when ordinary clients
// share their base transport. A changed proxy or TLS identity selects a fresh
// base transport and retires that credential's previous marketplace pools.
func (p *Pool) MarketClient(credentialID int64, selected *http.Client) (*http.Client, error) {
	client, err := MarketClient(selected)
	if err != nil {
		return nil, err
	}
	transport := client.Transport.(*marketTransport)
	if !reflect.TypeOf(transport.base).Comparable() {
		return nil, ErrMarketTransportPolicy
	}
	key := marketClientKey{credentialID: credentialID, base: transport.base}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.marketClock++
	if cached := p.marketClients[key]; cached != nil {
		cached.used = p.marketClock
		client.Transport = cached.transport
		return client, nil
	}
	if p.marketClients == nil {
		p.marketClients = make(map[marketClientKey]*marketClientEntry)
	}
	for oldKey, cached := range p.marketClients {
		if oldKey.credentialID == credentialID {
			cached.transport.closeIdle(true)
			delete(p.marketClients, oldKey)
		}
	}
	if len(p.marketClients) >= marketClientsPerPool {
		var oldestKey marketClientKey
		var oldest *marketClientEntry
		for candidateKey, candidate := range p.marketClients {
			if oldest == nil || candidate.used < oldest.used {
				oldestKey, oldest = candidateKey, candidate
			}
		}
		oldest.transport.closeIdle(true)
		delete(p.marketClients, oldestKey)
	}
	p.marketClients[key] = &marketClientEntry{transport: transport, used: p.marketClock}
	return client, nil
}

type marketTransport struct {
	base    http.RoundTripper
	lookup  func(context.Context, string, string) ([]netip.Addr, error)
	mu      sync.Mutex
	pinned  map[marketOrigin]*marketPinned
	clock   uint64
	retired bool
}

// DNS is still checked for every request. A pool is tied to its immutable
// selected transport and to one validated origin/address/proxy combination.
type marketOrigin struct {
	host, port, proxy string
	address           netip.Addr
}

type marketPinned struct {
	transport http.RoundTripper
	closeIdle func()
	used      uint64
	retired   bool
}

const marketOriginsPerClient = 8

func (t *marketTransport) acquire(req *http.Request, address netip.Addr) (*marketPinned, func(), error) {
	key := marketOrigin{host: strings.ToLower(req.URL.Hostname()), port: req.URL.Port(), address: address}
	if key.port == "" {
		key.port = "443"
	}
	var standard *http.Transport
	var proxy *url.URL
	if base, ok := t.base.(*http.Transport); ok {
		if err := validateImageBaseTransport(base); err != nil {
			return nil, nil, ErrMarketTransportPolicy
		}
		standard = base
		if base.Proxy != nil {
			var err error
			proxy, err = base.Proxy(req)
			if err != nil {
				return nil, nil, ErrMarketTransportPolicy
			}
		}
		if proxy != nil {
			key.proxy = proxy.String()
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clock++
	if entry := t.pinned[key]; entry != nil {
		entry.used = t.clock
		return entry, func() { t.release(entry) }, nil
	}
	var transport http.RoundTripper
	cleanup := func() {}
	var err error
	if custom, ok := t.base.(PinnedMarketTransport); ok {
		transport, err = custom.PublicMarketTransport(key.host, address)
		if idle, ok := transport.(interface{ CloseIdleConnections() }); ok {
			cleanup = idle.CloseIdleConnections
		}
	} else if standard != nil {
		// Proxy was resolved once above, including credentials, before looking
		// up the pool. Pin that decision rather than calling a dynamic selector
		// again while constructing a transport.
		selected := standard.Clone()
		selected.Proxy = http.ProxyURL(proxy)
		if selected.IdleConnTimeout <= 0 {
			selected.IdleConnTimeout = 90 * time.Second
		}
		transport, cleanup, err = imageTransport(req.Clone(req.Context()), address, selected)
	}
	if err != nil || transport == nil {
		cleanup()
		return nil, nil, ErrMarketTransportPolicy
	}
	entry := &marketPinned{transport: transport, closeIdle: cleanup, used: t.clock, retired: t.retired}
	if !t.retired {
		if t.pinned == nil {
			t.pinned = make(map[marketOrigin]*marketPinned)
		}
		if len(t.pinned) >= marketOriginsPerClient {
			var oldestKey marketOrigin
			var oldest *marketPinned
			for candidateKey, candidate := range t.pinned {
				if oldest == nil || candidate.used < oldest.used {
					oldestKey, oldest = candidateKey, candidate
				}
			}
			oldest.retired = true
			oldest.closeIdle()
			delete(t.pinned, oldestKey)
		}
		t.pinned[key] = entry
	}
	return entry, func() { t.release(entry) }, nil
}

func (t *marketTransport) release(entry *marketPinned) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if entry.retired {
		// A response can become idle after eviction. Close it on release as
		// well so an active stream cannot resurrect an evicted idle pool.
		entry.closeIdle()
	}
}

func (t *marketTransport) closeIdle(retire bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.retired = t.retired || retire
	for key, entry := range t.pinned {
		entry.retired = true
		entry.closeIdle()
		delete(t.pinned, key)
	}
}

func (t *marketTransport) CloseIdleConnections() { t.closeIdle(false) }

// PublicImageTransport keeps image fetching available through a selected
// marketplace client while isolating image requests from origin credentials.
func (t *marketTransport) PublicImageTransport(host string, address netip.Addr) (http.RoundTripper, error) {
	if t == nil || !PublicUpstreamAddress(address) || host == "" || strings.ContainsAny(host, "/\\?#@% \t\r\n\x00") {
		return nil, ErrImageTransportPolicy
	}
	if custom, ok := t.base.(PinnedImageTransport); ok {
		return custom.PublicImageTransport(host, address.Unmap())
	}
	if base, ok := t.base.(*http.Transport); ok {
		return &marketImageTransport{host: host, address: address.Unmap(), base: base}, nil
	}
	return nil, ErrImageTransportPolicy
}

type marketImageTransport struct {
	host    string
	address netip.Addr
	base    *http.Transport
}

func (t *marketImageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || req.Method != http.MethodGet || (req.Body != nil && req.Body != http.NoBody) ||
		req.URL.User != nil || req.URL.Fragment != "" || strings.Contains(req.URL.Host, "%") ||
		(req.URL.Scheme != "http" && req.URL.Scheme != "https") || !strings.EqualFold(req.URL.Hostname(), t.host) {
		return nil, ErrImageTransportPolicy
	}
	clone := req.Clone(req.Context())
	clone.Header = make(http.Header)
	clone.Header["Accept"] = append([]string(nil), req.Header.Values("Accept")...)
	clone.Trailer, clone.TransferEncoding, clone.ContentLength = nil, nil, 0
	pinned, cleanup, err := imageTransport(clone, t.address, t.base)
	if err != nil {
		return nil, err
	}
	response, err := pinned.RoundTrip(clone)
	if err != nil || response == nil || response.Body == nil {
		cleanup()
		if err == nil {
			err = ErrImageTransportPolicy
		}
		return nil, err
	}
	response.Body = &marketBody{ReadCloser: response.Body, cleanup: cleanup}
	return response, nil
}

func (t *marketTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || req.URL.Scheme != "https" || req.URL.Hostname() == "" ||
		(req.Host != "" && !strings.EqualFold(req.Host, req.URL.Host)) ||
		req.URL.User != nil || req.URL.Fragment != "" || strings.Contains(req.URL.Host, "%") ||
		(req.URL.Port() != "" && req.URL.Port() != "443" && req.URL.Port() != "8443") {
		return nil, ErrMarketTransportPolicy
	}
	addresses, err := t.lookup(req.Context(), "ip", req.URL.Hostname())
	if err != nil || len(addresses) == 0 {
		return nil, errors.Join(ErrMarketTransportPolicy, req.Context().Err())
	}
	for _, address := range addresses {
		if !PublicUpstreamAddress(address) {
			return nil, ErrMarketTransportPolicy
		}
	}
	clone := req.Clone(req.Context())
	clone.Header.Del("Proxy-Authorization")
	entry, release, err := t.acquire(clone, addresses[0].Unmap())
	if err != nil {
		return nil, ErrMarketTransportPolicy
	}
	if _, standard := t.base.(*http.Transport); standard {
		clone.Host = req.URL.Host
		port := req.URL.Port()
		if port == "" {
			port = "443"
		}
		clone.URL.Host = net.JoinHostPort(addresses[0].Unmap().String(), port)
	}
	// A body cleanup deadline must cancel only this request, never its caller
	// or another request that reuses the same pinned connection pool.
	ctx, cancel := context.WithCancelCause(clone.Context())
	clone = clone.WithContext(ctx)
	response, err := entry.transport.RoundTrip(clone)
	if err != nil {
		cancel(err)
		release()
		return nil, err
	}
	if response == nil || response.Body == nil {
		cancel(ErrMarketTransportPolicy)
		release()
		return nil, ErrMarketTransportPolicy
	}
	response.Body = &marketBody{ReadCloser: response.Body, cleanup: release, ctx: ctx, cancel: cancel,
		drain: response.StatusCode != http.StatusSwitchingProtocols}
	return response, nil
}

type marketBody struct {
	io.ReadCloser
	cleanup       func()
	once          sync.Once
	ctx           context.Context
	cancel        context.CancelCauseFunc
	drain         bool
	mu            sync.Mutex
	reading       int
	closing       bool
	eof           bool
	closeErr      error
	bodyCloseOnce sync.Once
	bodyCloseErr  error
}

const (
	marketBodyDrainBytes   = 32 << 10
	marketBodyDrainTimeout = 100 * time.Millisecond
)

var (
	ErrMarketBodyDrainLimit   = errors.New("httpx: marketplace response cleanup exceeds byte limit")
	ErrMarketBodyDrainTimeout = errors.New("httpx: marketplace response cleanup timed out")
)

func (b *marketBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	if b.closing {
		b.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	b.reading++
	b.mu.Unlock()
	n, err := b.ReadCloser.Read(p)
	b.mu.Lock()
	b.reading--
	b.eof = b.eof || errors.Is(err, io.EOF)
	b.mu.Unlock()
	return n, err
}

func (b *marketBody) Write(p []byte) (int, error) {
	writer, ok := b.ReadCloser.(io.Writer)
	if !ok {
		return 0, ErrMarketTransportPolicy
	}
	return writer.Write(p)
}

func (b *marketBody) Close() error {
	b.once.Do(func() {
		b.mu.Lock()
		b.closing = true
		shouldDrain := b.drain && !b.eof && b.reading == 0 && b.ctx.Err() == nil
		b.mu.Unlock()
		if shouldDrain {
			b.closeErr = b.drainTail()
		}
		if b.cancel != nil {
			b.cancel(context.Canceled)
		}
		b.closeErr = errors.Join(b.closeErr, b.closeUnderlying())
		b.cleanup()
	})
	return b.closeErr
}

func (b *marketBody) closeUnderlying() error {
	b.bodyCloseOnce.Do(func() { b.bodyCloseErr = b.ReadCloser.Close() })
	return b.bodyCloseErr
}

// Protocol terminal events such as SSE [DONE] precede the final HTTP chunk.
// Consuming a small tail to EOF lets net/http reuse that connection. A peer
// that keeps streaming or stalls is canceled and closed instead. Upgrades
// and image transports never enter this path. An active Read is canceled
// directly so Close cannot race it by adding a second reader.
func (b *marketBody) drainTail() error {
	timedOut := make(chan struct{})
	timer := time.AfterFunc(marketBodyDrainTimeout, func() {
		defer close(timedOut)
		b.cancel(ErrMarketBodyDrainTimeout)
		_ = b.closeUnderlying()
	})
	_, err := io.CopyN(io.Discard, b.ReadCloser, marketBodyDrainBytes+1)
	if !timer.Stop() {
		<-timedOut
	}
	if cause := context.Cause(b.ctx); cause != nil {
		return errors.Join(cause, err)
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		b.cancel(ErrMarketBodyDrainLimit)
		return ErrMarketBodyDrainLimit
	}
	return err
}
