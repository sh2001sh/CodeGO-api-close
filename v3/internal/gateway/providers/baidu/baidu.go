// Package baidu adapts the legacy Wenxin Workshop Chat API.
package baidu

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const ID = "baidu"

// Provider permits a dedicated OAuth client and endpoint for network policies.
// The zero value uses Baidu's endpoint and a shared expiring token cache.
type Provider struct {
	Client   *http.Client
	TokenURL string
	Cache    *TokenCache
}

func (p Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req == nil || req.Protocol != gateway.ProtocolOpenAIChat {
		return nil, invalid("unsupported_protocol", "baidu: only Chat is supported")
	}
	body, err := convertRequest(req.Body, req.Stream)
	if err != nil {
		return nil, err
	}
	model := target.UpstreamModel
	if model == "" {
		model = req.Model
	}
	token, err := p.accessToken(ctx, target.Secret)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(target.BaseURL, "/")
	if base == "" {
		base = "https://aip.baidubce.com"
	}
	u, err := url.Parse(base + "/rpc/2.0/ai_custom/v1/wenxinworkshop/chat/" + url.PathEscape(modelPath(model)))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("access_token", token)
	u.RawQuery = q.Encode()
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Content-Type", "application/json")
	if req.Stream {
		out.Header.Set("Accept", "text/event-stream")
	}
	return out, nil
}

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	return newResponseStream(req, resp)
}

func modelPath(model string) string {
	paths := map[string]string{
		"ERNIE-4.0": "completions_pro", "ERNIE-Bot-4": "completions_pro", "ERNIE-4.0-8K": "completions_pro",
		"ERNIE-Bot": "completions", "ERNIE-3.5-8K": "completions",
		"ERNIE-Bot-turbo": "eb-instant", "ERNIE-Lite-8K-0922": "eb-instant",
		"ERNIE-Speed": "ernie_speed", "ERNIE-Speed-8K": "ernie_speed",
		"ERNIE-3.5-8K-0205": "ernie-3.5-8k-0205", "ERNIE-3.5-8K-1222": "ernie-3.5-8k-1222",
		"ERNIE-Bot-8K": "ernie_bot_8k", "ERNIE-3.5-4K-0205": "ernie-3.5-4k-0205",
		"ERNIE-Speed-128K": "ernie-speed-128k", "ERNIE-Lite-8K-0308": "ernie-lite-8k",
		"ERNIE-Tiny-8K": "ernie-tiny-8k", "BLOOMZ-7B": "bloomz_7b1",
	}
	if path := paths[model]; path != "" {
		return path
	}
	return strings.ToLower(model)
}

func invalid(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: code, Message: message}
}

func upstream(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: code, Message: message}
}
