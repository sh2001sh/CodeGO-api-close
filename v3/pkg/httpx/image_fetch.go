package httpx

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const MaxImageBytes = 20 << 20

var ErrImageTransportPolicy = errors.New("httpx: image transport cannot preserve safe network policy")

type Image struct {
	Data     []byte
	MIMEType string
}

// PinnedImageTransport allows a custom TLS implementation to preserve its
// fingerprint while connecting only to the supplied validated address. The
// returned transport MUST pin that address, validate TLS against host, and
// carry no origin credentials. It must not resolve host again.
type PinnedImageTransport interface {
	PublicImageTransport(host string, address netip.Addr) (http.RoundTripper, error)
}

type ImageFetchConfig struct {
	Transport http.RoundTripper
	MaxBytes  int64
	Timeout   time.Duration
}

// FetchImage downloads an untrusted public image with a pinned DNS result,
// bounded size/time, no redirects and no inherited request/auth headers.
// Ordinary *http.Transport configuration (including its proxy and TLS roots)
// is preserved. Custom TLS transports must implement PinnedImageTransport.
func FetchImage(ctx context.Context, rawURL string, cfg ImageFetchConfig) (Image, error) {
	return fetchImage(ctx, rawURL, cfg, net.DefaultResolver.LookupNetIP)
}

func fetchImage(ctx context.Context, rawURL string, cfg ImageFetchConfig, lookup func(context.Context, string, string) ([]netip.Addr, error)) (Image, error) {
	u, err := parseImageURL(rawURL)
	if err != nil {
		return Image{}, err
	}
	cfg, err = normalizeImageFetchConfig(cfg)
	if err != nil {
		return Image{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Image{}, err
	}
	addresses, err := resolvePublicImageAddresses(ctx, u.Hostname(), lookup)
	if err != nil {
		return Image{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Image{}, errors.New("httpx: invalid image request")
	}
	transport, cleanup, err := imageTransport(req, addresses[0].Unmap(), cfg.Transport)
	if err != nil {
		return Image{}, err
	}
	defer cleanup()
	return doFetchImage(ctx, req, transport, cfg.MaxBytes)
}

// parseImageURL rejects URLs that could reach credentials, a non-HTTP
// scheme, or ambiguous hosts before any network access is attempted.
func parseImageURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Fragment != "" ||
		(u.Scheme != "https" && u.Scheme != "http") || strings.Contains(u.Host, "%") {
		return nil, errors.New("httpx: unsafe image URL")
	}
	return u, nil
}

// normalizeImageFetchConfig fills in defaults and enforces the bounds the
// rest of fetchImage assumes are already valid.
func normalizeImageFetchConfig(cfg ImageFetchConfig) (ImageFetchConfig, error) {
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = MaxImageBytes
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.MaxBytes < 1 || cfg.MaxBytes > 64<<20 || cfg.Timeout < 0 || cfg.Timeout > 30*time.Second {
		return cfg, errors.New("httpx: invalid image fetch bounds")
	}
	return cfg, nil
}

// resolvePublicImageAddresses looks up host and rejects it unless every
// resolved address is public, so pinning the first address can't be used to
// reach internal infrastructure via multi-answer DNS.
func resolvePublicImageAddresses(ctx context.Context, host string, lookup func(context.Context, string, string) ([]netip.Addr, error)) ([]netip.Addr, error) {
	addresses, err := lookup(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("httpx: image host lookup failed")
	}
	for _, address := range addresses {
		if !publicImageAddress(address) {
			return nil, errors.New("httpx: image host resolves to a nonpublic address")
		}
	}
	return addresses, nil
}

// doFetchImage issues the pinned request, enforces the size bound on both
// the declared and actual body length, and validates the sniffed content
// type against what the server declared.
func doFetchImage(ctx context.Context, req *http.Request, transport http.RoundTripper, maxBytes int64) (Image, error) {
	req.Header.Set("Accept", "image/*")
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Image{}, ctx.Err()
		}
		return Image{}, errors.New("httpx: image request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Image{}, errors.New("httpx: image source returned a non-success response")
	}
	if resp.ContentLength > maxBytes {
		return Image{}, errors.New("httpx: image exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return Image{}, ctx.Err()
		}
		return Image{}, errors.New("httpx: image response read failed")
	}
	if len(data) == 0 || int64(len(data)) > maxBytes {
		return Image{}, errors.New("httpx: image exceeds size limit or is empty")
	}
	contentType := imageContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		return Image{}, errors.New("httpx: image source did not contain a supported image")
	}
	if header := resp.Header.Get("Content-Type"); header != "" {
		declared, _, err := mime.ParseMediaType(header)
		if err != nil || (declared != contentType && declared != "application/octet-stream") {
			return Image{}, errors.New("httpx: image content type does not match body")
		}
	}
	return Image{Data: data, MIMEType: contentType}, nil
}

func imageTransport(req *http.Request, ip netip.Addr, configured http.RoundTripper) (http.RoundTripper, func(), error) {
	noop := func() {}
	if custom, ok := configured.(PinnedImageTransport); ok {
		return pinnedImageTransport(req, ip, custom)
	}
	if configured == nil {
		configured = &http.Transport{ForceAttemptHTTP2: true, TLSHandshakeTimeout: 10 * time.Second}
	}
	base, ok := configured.(*http.Transport)
	if !ok {
		return nil, noop, errors.Join(ErrImageTransportPolicy, errors.New("custom image transport requires public address pinning"))
	}
	if err := validateImageBaseTransport(base); err != nil {
		return nil, noop, err
	}
	cloned := base.Clone()
	proxy, err := resolveImageProxy(cloned, req)
	if err != nil {
		return nil, noop, err
	}
	configureImageTLSAndHost(cloned, req, ip)
	// net/http's HTTP proxy request line uses Request.Host, which would undo
	// pinning. Explicit tunnels also keep HTTPS proxy SNI separate from origin
	// SNI, so both certificates are verified against the correct host.
	if proxy != nil && (proxy.Scheme == "http" || proxy.Scheme == "https") {
		cloned.Proxy = nil
		cloned.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return imageTunnel(ctx, network, address, proxy, base)
		}
	}
	return cloned, cloned.CloseIdleConnections, nil
}

func pinnedImageTransport(req *http.Request, ip netip.Addr, custom PinnedImageTransport) (http.RoundTripper, func(), error) {
	noop := func() {}
	transport, err := custom.PublicImageTransport(req.URL.Hostname(), ip)
	if err != nil || transport == nil {
		return nil, noop, errors.Join(ErrImageTransportPolicy, errors.New("pinned image transport is unavailable"))
	}
	if idle, ok := transport.(interface{ CloseIdleConnections() }); ok {
		return transport, idle.CloseIdleConnections, nil
	}
	return transport, noop, nil
}

// validateImageBaseTransport rejects configurations that could bypass
// address pinning or TLS verification entirely.
func validateImageBaseTransport(base *http.Transport) error {
	//nolint:staticcheck // A configured deprecated TLS dial hook must also be rejected; it bypasses safe SNI/pinning.
	if base.DialTLSContext != nil || base.DialTLS != nil {
		return errors.Join(ErrImageTransportPolicy, errors.New("custom image TLS requires public address pinning"))
	}
	if base.TLSClientConfig != nil && base.TLSClientConfig.InsecureSkipVerify {
		return errors.Join(ErrImageTransportPolicy, errors.New("image TLS certificate verification is required"))
	}
	return nil
}

func resolveImageProxy(cloned *http.Transport, req *http.Request) (*url.URL, error) {
	if cloned.Proxy == nil {
		return nil, nil
	}
	proxy, err := cloned.Proxy(req)
	if err != nil {
		return nil, errors.New("httpx: image proxy selection failed")
	}
	cloned.Proxy = http.ProxyURL(proxy)
	return proxy, nil
}

// configureImageTLSAndHost pins req to ip while keeping SNI and the TLS
// verification host set to the original hostname.
func configureImageTLSAndHost(cloned *http.Transport, req *http.Request, ip netip.Addr) {
	host := req.URL.Hostname()
	if cloned.TLSClientConfig == nil {
		cloned.TLSClientConfig = &tls.Config{}
	}
	cloned.TLSClientConfig = cloned.TLSClientConfig.Clone()
	cloned.TLSClientConfig.ServerName = host
	port := req.URL.Port()
	if port == "" {
		if req.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	req.Host = req.URL.Host
	req.URL.Host = net.JoinHostPort(ip.String(), port)
}

func imageTunnel(ctx context.Context, network, address string, proxy *url.URL, base *http.Transport) (net.Conn, error) {
	port := proxy.Port()
	if port == "" {
		port = "80"
		if proxy.Scheme == "https" {
			port = "443"
		}
	}
	dial := base.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}
	conn, err := dial(ctx, network, net.JoinHostPort(proxy.Hostname(), port))
	if err != nil {
		return nil, err
	}
	underlying := conn
	stop := context.AfterFunc(ctx, func() { _ = underlying.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if proxy.Scheme == "https" {
		config := &tls.Config{ServerName: proxy.Hostname()}
		if base.TLSClientConfig != nil {
			config = base.TLSClientConfig.Clone()
			config.ServerName = proxy.Hostname()
		}
		secured := tls.Client(conn, config)
		if err := secured.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		conn = secured
	}
	connect := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: address}, Host: address, Header: base.ProxyConnectHeader.Clone()}
	if connect.Header == nil {
		connect.Header = make(http.Header)
	}
	if proxy.User != nil {
		password, _ := proxy.User.Password()
		connect.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(proxy.User.Username()+":"+password)))
	}
	if err := connect.Write(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), connect)
	if err != nil || response.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, errors.New("httpx: image proxy tunnel failed")
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func publicImageAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "100::/64", "2001::/32", "2001:db8::/32", "2002::/16"} {
		if netip.MustParsePrefix(raw).Contains(address) {
			return false
		}
	}
	return true
}

func imageContentType(data []byte) string {
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		switch string(data[8:12]) {
		case "heic", "heix", "hevc", "hevx":
			return "image/heic"
		case "mif1", "msf1":
			return "image/heif"
		case "avif", "avis":
			return "image/avif"
		}
	}
	return http.DetectContentType(data)
}
