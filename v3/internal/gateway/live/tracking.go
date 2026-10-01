package live

import (
	"context"
	"errors"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type locatorContextKey struct{}

// TrackingProvider persists native response IDs observed by the ordinary relay.
// Wrap outside the protocol bridge; routing secrets stay in the live snapshot.
func (h *Handler) TrackingProvider(provider gateway.Provider) gateway.Provider {
	return trackingProvider{handler: h, provider: provider}
}

type trackingProvider struct {
	handler  *Handler
	provider gateway.Provider
}

func (p trackingProvider) UpstreamTransport(req *gateway.Request, fallback http.RoundTripper) http.RoundTripper {
	if selector, ok := p.provider.(gateway.TransportProvider); ok {
		return selector.UpstreamTransport(req, fallback)
	}
	if native, ok := p.provider.(http.RoundTripper); ok {
		return native
	}
	return fallback
}

func (p trackingProvider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	prepared, err := p.prepareTrackedRequest(ctx, req, target)
	if err != nil {
		return nil, err
	}
	out, err := p.provider.BuildRequest(ctx, prepared, target)
	if err != nil {
		return nil, err
	}
	return out.WithContext(context.WithValue(out.Context(), locatorContextKey{}, target)), nil
}

func (p trackingProvider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	stream := p.provider.Decode(req, resp)
	if req.Protocol != gateway.ProtocolResponses {
		return stream
	}
	if resp.Request == nil {
		return &trackingStream{inner: stream, err: errors.New("live: response route context missing")}
	}
	target, ok := resp.Request.Context().Value(locatorContextKey{}).(gateway.Target)
	if !ok {
		return &trackingStream{inner: stream, err: errors.New("live: response route context missing")}
	}
	return &trackingStream{inner: stream, handler: p.handler, req: req, target: target, ctx: resp.Request.Context(), seen: make(map[string]bool)}
}

type trackingStream struct {
	inner   gateway.EventStream
	handler *Handler
	req     *gateway.Request
	target  gateway.Target
	ctx     context.Context
	seen    map[string]bool
	err     error
}

func (s *trackingStream) Next() (gateway.Event, error) {
	if s.err != nil {
		return gateway.Event{}, s.err
	}
	event, err := s.inner.Next()
	if err != nil {
		return event, err
	}
	id := gjson.GetBytes(event.Payload, "response.id").Str
	if id == "" {
		id = gjson.GetBytes(event.Payload, "id").Str
	}
	if id != "" && !s.seen[id] {
		ctx, cancel := context.WithTimeout(s.ctx, s.handler.cfg.FinalizeTimeout)
		err := s.handler.remember(ctx, s.req, s.target, id)
		cancel()
		if err != nil {
			s.err = err
			return gateway.Event{Kind: gateway.EventError, Err: &gateway.UpstreamError{Status: 503, Code: "response_store_unavailable", Message: "response routing persistence is unavailable"}}, nil
		}
		s.seen[id] = true
	}
	return event, nil
}

func (s *trackingStream) Close() error { return s.inner.Close() }
