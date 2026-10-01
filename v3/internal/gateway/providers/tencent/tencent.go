// Package tencent adapts the signed Tencent Hunyuan ChatCompletions API.
package tencent

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const ID = "tencent"
const maxJSONBody = 64 << 20

type Provider struct{}

func (Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req.Protocol != gateway.ProtocolOpenAIChat {
		return nil, invalid("unsupported_protocol", "tencent: unsupported client protocol")
	}
	secretID, secretKey, err := credentials(target.Secret)
	if err != nil {
		return nil, err
	}
	model := target.UpstreamModel
	if model == "" {
		model = req.Model
	}
	body, err := convertRequest(req.Body, model, req.Stream)
	if err != nil {
		return nil, invalid("unsupported_request", err.Error())
	}
	base := target.BaseURL
	if base == "" {
		base = "https://hunyuan.tencentcloudapi.com"
	}
	endpoint, err := url.Parse(strings.TrimRight(base, "/") + "/")
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, upstream("invalid_endpoint", "tencent: invalid endpoint")
	}
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, upstream("invalid_endpoint", "tencent: invalid endpoint")
	}
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("X-TC-Action", "ChatCompletions")
	out.Header.Set("X-TC-Version", "2023-09-01")
	timestamp := time.Now().Unix()
	out = out.WithContext(context.WithValue(out.Context(), signingTimestampKey{}, timestamp))
	out.Header.Set("X-TC-Timestamp", strconv.FormatInt(timestamp, 10))
	out.Header.Set("Authorization", signature(out, body, secretID, secretKey, timestamp))
	if req.Stream {
		out.Header.Set("Accept", "text/event-stream")
	}
	return out, nil
}

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	return newResponseStream(req, resp)
}

func credentials(secret string) (string, string, error) {
	parts := strings.Split(strings.TrimPrefix(secret, "Bearer "), "|")
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" || strings.ContainsAny(parts[1], "\r\n, /\t") || strings.ContainsAny(parts[2], "\r\n") {
		return "", "", upstream("invalid_credentials", "tencent: expected appID|secretID|secretKey credentials")
	}
	if id, err := strconv.ParseInt(parts[0], 10, 64); err != nil || id <= 0 {
		return "", "", upstream("invalid_credentials", "tencent: invalid application ID")
	}
	return parts[1], parts[2], nil
}

func invalid(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: code, Message: message}
}

func upstream(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: code, Message: message}
}
