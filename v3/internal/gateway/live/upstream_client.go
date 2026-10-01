package live

import (
	"context"
	"errors"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func (h *Handler) upstreamClient(ctx context.Context, target gateway.Target) (*http.Client, error) {
	configured := h.cfg.Client
	if h.cfg.Clients != nil {
		var err error
		configured, err = h.cfg.Clients(ctx, target)
		if err != nil {
			return nil, err
		}
		if configured == nil {
			return nil, errors.New("live: configured upstream client is unavailable")
		}
	}
	client := *configured
	if h.cfg.Clients == nil && target.ProxyURL != "" {
		transport, err := backgroundTransports.Transport(target.ProxyURL)
		if err != nil {
			return nil, err
		}
		client.Transport = transport
	}
	if client.Transport == nil {
		client.Transport = http.DefaultTransport
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client, nil
}
