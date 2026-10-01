// Package ali preserves DashScope's Chat, Responses and Messages endpoints and
// channel plugin metadata while reusing the canonical protocol adapters.
package ali

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/anthropic"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/bridge"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
)

const ID = "ali"

type Provider struct {
	// AnthropicModelPatterns preserves ALI_ANTHROPIC_MESSAGES_MODELS semantics.
	// Nil uses v2's defaults; an empty non-nil list disables native Messages.
	// Patterns are case-insensitive substrings of the resolved upstream model.
	AnthropicModelPatterns []string
}

func (p Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	plugin, err := channelPlugin(target.Settings)
	if err != nil {
		return nil, err
	}
	delegate, path, bridged, err := p.routeProtocol(req, target)
	if bridged || err != nil {
		// v2 converts unsupported native Anthropic models through the Chat
		// endpoint. Reuse the real bridge, retaining tools and billing usage.
		if bridged {
			return (bridge.Provider{Chat: p}).BuildRequest(ctx, req, target)
		}
		return nil, err
	}
	endpoint, err := resolveEndpoint(target.BaseURL, path)
	if err != nil {
		return nil, err
	}
	canonical := target
	canonical.BaseURL = "https://ali.invalid"
	out, err := delegate.BuildRequest(ctx, req, canonical)
	if err != nil {
		return nil, err
	}
	out.URL = endpoint
	out.Host = ""
	if req.Protocol == gateway.ProtocolAnthropic {
		// DashScope's Messages endpoint uses its channel Bearer credential,
		// rather than Anthropic's x-api-key authentication.
		out.Header.Del("X-Api-Key")
		out.Header.Set("Authorization", "Bearer "+target.Secret)
	}
	if plugin != "" {
		out.Header.Set("X-DashScope-Plugin", plugin)
	}
	if req.Stream {
		out.Header.Set("X-DashScope-SSE", "enable")
	}
	return out, nil
}

// routeProtocol selects the canonical adapter and DashScope path for req's
// client protocol. bridged reports that the Anthropic-protocol request
// targets a model DashScope cannot serve natively and must instead go
// through the Chat-compatible bridge; delegate and path are unset in that
// case and the caller must not use them.
func (p Provider) routeProtocol(req *gateway.Request, target gateway.Target) (delegate gateway.Provider, path string, bridged bool, err error) {
	switch req.Protocol {
	case gateway.ProtocolOpenAIChat:
		return openai.Provider{}, "/compatible-mode/v1/chat/completions", false, nil
	case gateway.ProtocolResponses:
		return responses.Provider{}, "/api/v2/apps/protocols/compatible-mode/v1/responses", false, nil
	case gateway.ProtocolAnthropic:
		model := target.UpstreamModel
		if model == "" {
			model = req.Model
		}
		if !p.supportsAnthropic(model) {
			return nil, "", true, nil
		}
		return anthropic.Provider{}, "/apps/anthropic/v1/messages", false, nil
	default:
		return nil, "", false, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "unsupported_protocol", Message: "ali: unsupported client protocol"}
	}
}

func resolveEndpoint(base, path string) (*url.URL, error) {
	if base == "" {
		base = "https://dashscope.aliyuncs.com"
	}
	endpoint, err := url.Parse(base)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil {
		return nil, errors.New("ali: base URL must be an absolute HTTP URL without credentials")
	}
	endpoint.Path = endpointPath(endpoint.Path, path)
	endpoint.RawPath = ""
	return endpoint, nil
}

func (p Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	if req.Protocol == gateway.ProtocolResponses {
		return (responses.Provider{}).Decode(req, resp)
	}
	if req.Protocol == gateway.ProtocolAnthropic {
		// http.Client records the actual upstream request. It distinguishes
		// the model-dependent v2 Chat fallback without shared mutable state.
		if resp.Request != nil && resp.Request.URL != nil && strings.HasSuffix(resp.Request.URL.Path, "/compatible-mode/v1/chat/completions") {
			return (bridge.Provider{Chat: p}).Decode(req, resp)
		}
		return (anthropic.Provider{}).Decode(req, resp)
	}
	return (openai.Provider{}).Decode(req, resp)
}

func (p Provider) supportsAnthropic(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	patterns := p.AnthropicModelPatterns
	if patterns == nil {
		patterns = []string{"qwen", "deepseek-v4", "kimi", "glm", "minimax-m"}
	}
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern != "" && strings.Contains(model, pattern) {
			return true
		}
	}
	return false
}

func channelPlugin(settings map[string]any) (string, error) {
	value, exists := settings["plugin"]
	if !exists || value == nil {
		return "", nil
	}
	plugin, ok := value.(string)
	if !ok || strings.ContainsAny(plugin, "\r\n") {
		return "", &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: "invalid_channel_plugin", Message: "ali: invalid channel plugin"}
	}
	return plugin, nil
}

func endpointPath(base, path string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, path) {
		return base
	}
	for _, suffix := range []string{
		"/apps/anthropic/v1/messages", "/apps/anthropic/v1", "/apps/anthropic",
		"/api/v2/apps/protocols/compatible-mode/v1/responses", "/api/v2/apps/protocols/compatible-mode/v1", "/api/v2/apps/protocols/compatible-mode",
		"/compatible-mode/v1/chat/completions", "/compatible-mode/v1", "/compatible-mode",
	} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimSuffix(base, suffix)
			break
		}
	}
	return base + path
}
