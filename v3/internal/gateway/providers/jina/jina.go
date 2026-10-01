// Package jina builds Jina's native embeddings and rerank requests. Jina does
// not provide Chat, Responses, Anthropic Messages or Gemini generation APIs.
package jina

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const ID = "jina"

type Provider struct{}

func (Provider) BuildRequest(context.Context, *gateway.Request, gateway.Target) (*http.Request, error) {
	return nil, invalid("unsupported_protocol", "jina supports embeddings and rerank only")
}

func (Provider) Decode(_ *gateway.Request, resp *http.Response) gateway.EventStream {
	return &unsupportedStream{body: resp.Body}
}

// BuildEmbeddingRequest preserves Jina's native task, dimensions and multimodal
// inputs, removing encoding_format as v2 did because Jina returns float vectors.
func BuildEmbeddingRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	fields, err := requestFields(req)
	if err != nil {
		return nil, err
	}
	input, exists := fields["input"]
	if !exists || bytes.Equal(bytes.TrimSpace(input), []byte("null")) {
		return nil, invalid("invalid_input", "jina embeddings requires input")
	}
	delete(fields, "encoding_format")
	return build(ctx, req, target, "/v1/embeddings", fields)
}

// BuildRerankRequest preserves string and object documents without changing
// their order, plus Jina-specific options such as return_documents and task.
func BuildRerankRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	fields, err := requestFields(req)
	if err != nil {
		return nil, err
	}
	var query string
	if json.Unmarshal(fields["query"], &query) != nil || query == "" {
		return nil, invalid("invalid_query", "jina rerank requires a nonempty query")
	}
	var docs []json.RawMessage
	if json.Unmarshal(fields["documents"], &docs) != nil || len(docs) == 0 {
		return nil, invalid("invalid_documents", "jina rerank requires documents")
	}
	return build(ctx, req, target, "/v1/rerank", fields)
}

func requestFields(req *gateway.Request) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if req == nil || json.Unmarshal(req.Body, &fields) != nil || fields == nil {
		return nil, invalid("invalid_json", "jina request must be a JSON object")
	}
	return fields, nil
}

func build(ctx context.Context, req *gateway.Request, target gateway.Target, path string, fields map[string]json.RawMessage) (*http.Request, error) {
	model := target.UpstreamModel
	if model == "" {
		model = req.Model
	}
	if model == "" {
		return nil, invalid("invalid_model", "jina requires a model")
	}
	if strings.TrimSpace(target.Secret) == "" {
		return nil, configuration("invalid_credentials", "jina requires an API key")
	}
	base, err := url.Parse(strings.TrimRight(target.BaseURL, "/"))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, configuration("invalid_endpoint", "jina endpoint must be an HTTP base URL")
	}
	base.Path = strings.TrimSuffix(base.Path, "/v1") + path
	base.RawPath = ""
	fields["model"], _ = json.Marshal(model)
	body, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Authorization", "Bearer "+target.Secret)
	out.Header.Set("Content-Type", "application/json")
	return out, nil
}

func invalid(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: code, Message: message}
}

func configuration(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: code, Message: message}
}

type unsupportedStream struct {
	body io.ReadCloser
	done bool
}

func (s *unsupportedStream) Next() (gateway.Event, error) {
	if s.done {
		return gateway.Event{}, io.EOF
	}
	s.done = true
	return gateway.Event{Kind: gateway.EventError, Err: invalid("unsupported_protocol", "jina supports embeddings and rerank only")}, nil
}

func (s *unsupportedStream) Close() error { return s.body.Close() }
