// Package cohere adapts the native Cohere v2 Chat API to OpenAI Chat callers.
package cohere

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const ID = "cohere"
const maxJSONBody = 64 << 20
const maxStreamEvent = 8 << 20

type Provider struct{}

func (Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req.Protocol != gateway.ProtocolOpenAIChat {
		return nil, invalid("unsupported_protocol", "cohere: unsupported client protocol")
	}
	model := target.UpstreamModel
	if model == "" {
		model = req.Model
	}
	body, err := convertRequest(req.Body, model, req.Stream)
	if err != nil {
		return nil, invalid("unsupported_request", err.Error())
	}
	base := strings.TrimRight(target.BaseURL, "/")
	base = strings.TrimSuffix(strings.TrimSuffix(base, "/v1"), "/v2")
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v2/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("Authorization", "Bearer "+target.Secret)
	if req.Stream {
		out.Header.Set("Accept", "text/event-stream")
	}
	return out, nil
}

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	return newResponseStream(req, resp)
}
func invalid(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: code, Message: message}
}
func upstream(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: code, Message: message}
}
