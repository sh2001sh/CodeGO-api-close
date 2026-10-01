// Package cloudflare adapts the native Cloudflare Workers AI run API to Chat.
package cloudflare

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

const ID = "cloudflare"
const maxJSONBody = 64 << 20

type Provider struct{}

func (Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req.Protocol != gateway.ProtocolOpenAIChat {
		return nil, requestError("unsupported_protocol", "cloudflare: only Chat is supported")
	}
	account, token, ok := strings.Cut(target.Secret, "|")
	if !ok || !validSegment(account) || token == "" || strings.ContainsAny(token, "|\r\n") {
		return nil, configurationError("invalid_credentials", "cloudflare: credentials must have account|token format")
	}
	model := req.Model
	if target.UpstreamModel != "" {
		model = target.UpstreamModel
	}
	for _, part := range strings.Split(model, "/") {
		if !validSegment(strings.TrimPrefix(part, "@")) {
			return nil, requestError("unsupported_request", "cloudflare: invalid Workers AI model path")
		}
	}
	body, err := convertRequest(req.Body, req.Stream)
	if err != nil {
		return nil, requestError("unsupported_request", err.Error())
	}
	base := strings.TrimRight(target.BaseURL, "/")
	if base == "" {
		base = "https://api.cloudflare.com"
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, configurationError("invalid_base_url", "cloudflare: base URL must be an HTTP URL without credentials, query or fragment")
	}
	if !strings.HasSuffix(base, "/client/v4") {
		base += "/client/v4"
	}
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/accounts/"+account+"/ai/run/"+model, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("Authorization", "Bearer "+token)
	if req.Stream {
		out.Header.Set("Accept", "text/event-stream")
	}
	return out, nil
}

func validSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, c := range value {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-', c == '.':
		default:
			return false
		}
	}
	return true
}

func requestError(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: code, Message: message}
}

func configurationError(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: code, Message: message}
}

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	s := &responseStream{body: resp.Body, stream: req.Stream, model: req.Model,
		id: "chatcmpl-" + req.ID, created: req.Received.Unix(), wantUsage: clientWantsUsage(req.Body)}
	if req.Stream {
		s.reader = sse.NewReader(resp.Body, 0)
	}
	return s
}

var _ gateway.Provider = Provider{}

func unsupported(field string) error {
	return fmt.Errorf("cloudflare: unsupported Chat field %q", field)
}
