package httpx

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// MediaFetchConfig preserves the selected channel's proxy/TLS transport. Only
// the explicitly configured upstream origin may use its ordinary network
// policy; third-party result URLs must resolve entirely to public addresses.
type MediaFetchConfig struct {
	Client        *http.Client
	TrustedOrigin string
	MaxBytes      int64
	Timeout       time.Duration
}

// FetchMedia downloads an untrusted provider result with a bounded body,
// no redirects, cookies or inherited request credentials, and DNS pinning for
// external origins. It supports images, audio and other binary media.
func FetchMedia(ctx context.Context, rawURL string, cfg MediaFetchConfig) ([]byte, error) {
	return fetchMedia(ctx, rawURL, cfg, net.DefaultResolver.LookupNetIP)
}

func fetchMedia(ctx context.Context, rawURL string, cfg MediaFetchConfig, lookup func(context.Context, string, string) ([]netip.Addr, error)) ([]byte, error) {
	u, err := parseImageURL(rawURL)
	if err != nil {
		return nil, errors.New("httpx: unsafe media URL")
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = 64 << 20
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxBytes < 1 || cfg.MaxBytes > 64<<20 || cfg.Timeout < 0 || cfg.Timeout > 30*time.Second {
		return nil, errors.New("httpx: invalid media fetch bounds")
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("httpx: invalid media request")
	}
	client := http.Client{}
	if cfg.Client != nil {
		client = *cfg.Client
	}
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if !sameMediaOrigin(u, cfg.TrustedOrigin) {
		addresses, err := resolvePublicImageAddresses(ctx, u.Hostname(), lookup)
		if err != nil {
			return nil, err
		}
		transport, cleanup, err := imageTransport(req, addresses[0].Unmap(), client.Transport)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		client.Transport = transport
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("httpx: media request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > cfg.MaxBytes {
		return nil, errors.New("httpx: media response rejected")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, cfg.MaxBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("httpx: media response read failed")
	}
	if len(data) == 0 || int64(len(data)) > cfg.MaxBytes {
		return nil, errors.New("httpx: media exceeds size limit or is empty")
	}
	return data, nil
}

func sameMediaOrigin(u *url.URL, raw string) bool {
	base, err := parseImageURL(strings.TrimSpace(raw))
	return err == nil && strings.EqualFold(base.Scheme, u.Scheme) && strings.EqualFold(base.Host, u.Host)
}
