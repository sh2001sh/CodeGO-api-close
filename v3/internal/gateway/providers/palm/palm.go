// Package palm adapts the legacy Google PaLM generateMessage API to Chat.
package palm

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const ID = "palm"

type Provider struct{}

var modelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req == nil || req.Protocol != gateway.ProtocolOpenAIChat {
		return nil, invalid("unsupported_protocol", "PaLM supports Chat requests only")
	}
	if target.Secret == "" {
		return nil, invalid("missing_api_key", "PaLM requires an API key")
	}
	body, err := convertRequest(req.Body)
	if err != nil {
		return nil, invalid("unsupported_request", err.Error())
	}
	model := target.UpstreamModel
	if model == "" {
		model = req.Model
	}
	switch strings.ToLower(model) {
	case "", "palm-2", "palm2":
		model = "chat-bison-001"
	}
	model = strings.TrimPrefix(model, "models/")
	if !modelName.MatchString(model) {
		return nil, invalid("invalid_model", "PaLM model must be a model identifier")
	}
	base := target.BaseURL
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return nil, invalid("invalid_base_url", "PaLM requires an absolute HTTP base URL")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/v1beta2") {
		u.Path += "/v1beta2"
	}
	u.Path += "/models/" + model + ":generateMessage"
	u.RawPath = ""
	query := u.Query()
	query.Set("key", target.Secret)
	u.RawQuery = query.Encode()
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("Accept", "application/json") // PaLM has no native SSE endpoint.
	return out, nil
}

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	return &responseStream{req: req, response: resp, body: resp.Body}
}

func invalid(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: code, Message: message}
}

func upstream(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: code, Message: message}
}
