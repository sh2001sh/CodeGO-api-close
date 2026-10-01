// Package bridge converts native API callers through a Chat upstream when the
// channel has no adapter for the caller's protocol. Native adapters take priority.
package bridge

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type Provider struct {
	Chat   gateway.Provider
	Native map[gateway.Protocol]gateway.Provider
}

func (p Provider) FinalizeRequest(ctx context.Context, out *http.Request, req *gateway.Request, target gateway.Target) error {
	selected := p.Native[req.Protocol]
	if selected == nil {
		selected = p.Chat
	}
	return gateway.FinalizeProviderRequest(ctx, selected, out, req, target)
}

func (p Provider) BuildRawRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if native := p.Native[req.Protocol]; native != nil {
		return gateway.BuildProviderRequest(ctx, native, req, target)
	}
	if p.Chat == nil {
		return nil, errors.New("bridge: no Chat upstream adapter configured")
	}
	upstream := *req
	upstream.Protocol = gateway.ProtocolOpenAIChat
	return gateway.BuildProviderRequest(ctx, p.Chat, &upstream, target)
}

func (p Provider) UpstreamTransport(req *gateway.Request, fallback http.RoundTripper) http.RoundTripper {
	selected := p.Native[req.Protocol]
	if selected == nil {
		selected = p.Chat
	}
	if selector, ok := selected.(gateway.TransportProvider); ok {
		return selector.UpstreamTransport(req, fallback)
	}
	if native, ok := selected.(http.RoundTripper); ok {
		return native
	}
	return fallback
}

func (p Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if native := p.Native[req.Protocol]; native != nil {
		return native.BuildRequest(ctx, req, target)
	}
	if p.Chat == nil {
		return nil, errors.New("bridge: no Chat upstream adapter configured")
	}
	if req.Protocol == gateway.ProtocolOpenAIChat {
		return p.Chat.BuildRequest(ctx, req, target)
	}
	converted, err := normalize(req)
	if err != nil {
		return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "unsupported_protocol_conversion", Message: err.Error()}
	}
	return p.Chat.BuildRequest(ctx, converted, target)
}

func (p Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	if native := p.Native[req.Protocol]; native != nil {
		return native.Decode(req, resp)
	}
	if p.Chat == nil {
		return &failedStream{err: errors.New("bridge: no Chat upstream adapter configured"), body: resp.Body}
	}
	if req.Protocol == gateway.ProtocolOpenAIChat {
		return p.Chat.Decode(req, resp)
	}
	converted, err := normalize(req)
	if err != nil {
		return &failedStream{err: err, body: resp.Body}
	}
	return newStream(req, p.Chat.Decode(converted, resp))
}

type failedStream struct {
	err  error
	body io.Closer
}

func (s *failedStream) Next() (gateway.Event, error) {
	if s.err == nil {
		return gateway.Event{}, io.EOF
	}
	err := s.err
	s.err = nil
	return gateway.Event{}, err
}
func (s *failedStream) Close() error {
	if s.body == nil {
		return nil
	}
	return s.body.Close()
}
