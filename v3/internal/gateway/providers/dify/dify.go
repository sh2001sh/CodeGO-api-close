// Package dify adapts Dify's native Chat application API to OpenAI Chat.
package dify

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
	"github.com/tidwall/gjson"
)

const ID = "dify"
const maxResponseSize = 16 << 20

// Client can replace the default transport for standalone use. The gateway
// injects its configured channel transport through WithTransport.
type Provider struct {
	Client   *http.Client
	injected bool
}

type uploadOptions struct {
	images []inlineImage
	proxy  *url.URL
}

type uploadKey struct{}

func (Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req.Protocol != gateway.ProtocolOpenAIChat {
		return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "unsupported_protocol", Message: "dify: only Chat requests are supported"}
	}
	body, images, err := convertRequest(req)
	if err != nil {
		return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "unsupported_request", Message: err.Error()}
	}
	base := strings.TrimRight(target.BaseURL, "/")
	if base == "" {
		base = "https://api.dify.ai"
	}
	base = strings.TrimSuffix(base, "/v1")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "http" && u.Scheme != "https" {
		return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "invalid_endpoint", Message: "dify: endpoint must be an HTTP(S) base URL"}
	}
	opts := uploadOptions{images: images}
	if target.ProxyURL != "" {
		opts.proxy, err = url.Parse(target.ProxyURL)
		if err != nil || opts.proxy.Host == "" || opts.proxy.Scheme != "http" && opts.proxy.Scheme != "https" && opts.proxy.Scheme != "socks5" && opts.proxy.Scheme != "socks5h" {
			return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "invalid_proxy", Message: "dify: invalid channel proxy"}
		}
	}
	out, err := http.NewRequestWithContext(context.WithValue(ctx, uploadKey{}, opts), http.MethodPost, base+"/v1/chat-messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Authorization", "Bearer "+target.Secret)
	out.Header.Set("Content-Type", "application/json")
	if req.Stream {
		out.Header.Set("Accept", "text/event-stream")
	}
	return out, nil
}

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	s := &responseStream{body: resp.Body, stream: req.Stream, model: req.Model, id: "chatcmpl-" + req.ID,
		created: req.Received.Unix(), wantUsage: gjson.GetBytes(req.Body, "stream_options.include_usage").Bool()}
	if req.Stream {
		s.reader = sse.NewReader(resp.Body, maxResponseSize)
	}
	return s
}
