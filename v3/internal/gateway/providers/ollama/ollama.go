// Package ollama adapts Ollama's native Chat API to OpenAI Chat clients.
package ollama

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const ID = "ollama"
const maxJSONBody = 64 << 20

type Provider struct{}

func (Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req.Protocol != gateway.ProtocolOpenAIChat {
		return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error",
			Code: "unsupported_protocol", Message: fmt.Sprintf("ollama: unsupported client protocol %d", req.Protocol)}
	}
	model := req.Model
	if target.UpstreamModel != "" {
		model = target.UpstreamModel
	}
	body, err := convertRequest(req.Body, model, req.Stream)
	if err != nil {
		return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error",
			Code: "unsupported_request", Message: err.Error()}
	}
	base := strings.TrimRight(target.BaseURL, "/")
	base = strings.TrimSuffix(base, "/v1")
	base = strings.TrimSuffix(base, "/api")
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Content-Type", "application/json")
	if target.Secret != "" {
		out.Header.Set("Authorization", "Bearer "+target.Secret)
	}
	if req.Stream {
		out.Header.Set("Accept", "application/x-ndjson")
	}
	return out, nil
}

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	s := &responseStream{body: resp.Body, model: req.Model, id: "chatcmpl-" + req.ID,
		created: req.Received.Unix(), wantUsage: clientWantsUsage(req.Body), stream: req.Stream}
	s.init()
	return s
}
