package auxiliary

import (
	"context"
	"errors"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func (h *Handler) channelClient(ctx context.Context, target gateway.Target) (*http.Client, error) {
	if h.cfg.Clients != nil {
		configured, err := h.cfg.Clients(ctx, target)
		if err != nil {
			return nil, err
		}
		if configured == nil {
			return nil, errors.New("auxiliary: configured upstream client unavailable")
		}
		client := *configured
		if client.Transport == nil {
			if target.Fingerprint.TLSProfile != "" {
				return nil, errors.New("auxiliary: selected custom TLS transport unavailable")
			}
			client.Transport = http.DefaultTransport
		}
		client.CheckRedirect = noRedirect
		return &client, nil
	}
	transport, err := h.cfg.Transports.Transport(target.ProxyURL)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: transport, CheckRedirect: noRedirect}, nil
}
