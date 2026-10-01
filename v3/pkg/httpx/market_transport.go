package httpx

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
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

type marketTransport struct {
	base   http.RoundTripper
	lookup func(context.Context, string, string) ([]netip.Addr, error)
}

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
	var pinned http.RoundTripper
	cleanup := func() {}
	if custom, ok := t.base.(PinnedMarketTransport); ok {
		pinned, err = custom.PublicMarketTransport(req.URL.Hostname(), addresses[0].Unmap())
		if err == nil && pinned != nil {
			if idle, ok := pinned.(interface{ CloseIdleConnections() }); ok {
				cleanup = idle.CloseIdleConnections
			}
		}
	} else {
		pinned, cleanup, err = imageTransport(clone, addresses[0].Unmap(), t.base)
	}
	if err != nil || pinned == nil {
		cleanup()
		return nil, ErrMarketTransportPolicy
	}
	response, err := pinned.RoundTrip(clone)
	if err != nil {
		cleanup()
		return nil, err
	}
	if response == nil || response.Body == nil {
		cleanup()
		return nil, ErrMarketTransportPolicy
	}
	response.Body = &marketBody{ReadCloser: response.Body, cleanup: cleanup}
	return response, nil
}

type marketBody struct {
	io.ReadCloser
	cleanup func()
	once    sync.Once
}

func (b *marketBody) Write(p []byte) (int, error) {
	writer, ok := b.ReadCloser.(io.Writer)
	if !ok {
		return 0, ErrMarketTransportPolicy
	}
	return writer.Write(p)
}

func (b *marketBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.cleanup)
	return err
}
