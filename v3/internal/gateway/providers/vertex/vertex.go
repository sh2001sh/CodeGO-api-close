// Package vertex routes Anthropic, Gemini and open-source Vertex models while
// sharing the native adapters' request conversion, stream framing and usage.
package vertex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/anthropic"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/gemini"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
)

const ID = "vertex"

type Provider struct {
	HTTPClient     *http.Client // OAuth fallback; nil uses a bounded client and Target.ProxyURL.
	Clients        gateway.ClientProvider
	ImageTransport gemini.ImageTransport
	TokenEndpoint  string // Override for private OAuth endpoints or tests.
	Now            func() time.Time
	tokens         *tokenCache
}

type family uint8

const (
	google family = iota
	claude
	openSource
	imagen
)

func modelFamily(model string) family {
	if strings.HasPrefix(model, "imagen") {
		return imagen
	}
	if strings.HasPrefix(model, "claude") {
		return claude
	}
	if strings.Contains(model, "llama") || strings.Contains(model, "-maas") {
		return openSource
	}
	return google
}

func (p Provider) adapter(f family) gateway.Provider {
	switch f {
	case claude:
		return anthropic.Provider{}
	case openSource:
		return openai.Provider{}
	default:
		return gemini.Provider{ImageTransport: p.ImageTransport}
	}
}

func (p Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req == nil {
		return nil, errors.New("vertex: missing request")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := credentials(target.Secret)
	if err != nil {
		return nil, err
	}
	model, f, err := resolveModel(req, target)
	if err != nil {
		return nil, err
	}
	u, err := routedURL(target, c, req, model, f)
	if err != nil {
		return nil, err
	}
	out, err := p.buildDelegateRequest(ctx, req, target, model, f)
	if err != nil {
		return nil, err
	}
	if f == claude {
		if err := rewriteClaudeBody(out, req.Stream); err != nil {
			return nil, err
		}
	}
	out.URL = u
	if err := p.authenticate(ctx, out, target, c); err != nil {
		return nil, err
	}
	if f == claude {
		for key, value := range req.ClientHeaders {
			if strings.EqualFold(key, "Anthropic-Beta") {
				out.Header.Set("Anthropic-Beta", value)
			}
		}
	}
	return out, nil
}

// resolveModel determines the upstream model name and provider family for
// req, and rejects client protocols the model's family cannot serve.
func resolveModel(req *gateway.Request, target gateway.Target) (model string, f family, err error) {
	model = req.Model
	if target.UpstreamModel != "" {
		model = target.UpstreamModel
	}
	model = strings.TrimPrefix(model, "models/")
	f = modelFamily(model)
	supported := req.Protocol == gateway.ProtocolOpenAIChat || req.Protocol == gateway.ProtocolAnthropic && f == claude ||
		req.Protocol == gateway.ProtocolGemini && (f == google || f == imagen)
	if !supported {
		return "", 0, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error",
			Code: "unsupported_protocol", Message: "Vertex model does not support this client protocol"}
	}
	return model, f, nil
}

// routedURL resolves the configured region and builds the upstream model URL.
func routedURL(target gateway.Target, c Credentials, req *gateway.Request, model string, f family) (*url.URL, error) {
	region, err := configuredRegion(target.Settings, c, req.Model)
	if err != nil {
		return nil, err
	}
	routingCredentials := c
	routingCredentials.Region, routingCredentials.Regions = region, nil
	return modelURL(target.BaseURL, routingCredentials, req.Model, model, f, req.Stream)
}

// buildDelegateRequest converts the request body via the family-specific
// adapter (or the Imagen builder), using a target rewritten to look native
// to that adapter.
func (p Provider) buildDelegateRequest(ctx context.Context, req *gateway.Request, target gateway.Target, model string, f family) (*http.Request, error) {
	if f == imagen {
		return buildImagenRequest(ctx, req)
	}
	convertedTarget := target
	convertedTarget.BaseURL, convertedTarget.Secret, convertedTarget.UpstreamModel = "https://vertex.invalid", "", model
	// Vertex uses this legacy key for regions. Gemini's same-named setting
	// selects API versions and must not receive the Vertex region/map.
	convertedTarget.Settings = maps.Clone(target.Settings)
	delete(convertedTarget.Settings, "api_version")
	delegate := p
	if p.ImageTransport != nil {
		delegate.ImageTransport = func(ctx context.Context, _ gateway.Target) (http.RoundTripper, error) {
			return p.ImageTransport(ctx, target)
		}
	}
	return delegate.adapter(f).BuildRequest(ctx, req, convertedTarget)
}

// authenticate strips the delegate adapter's auth headers and applies
// Vertex's own API-key or OAuth credentials plus the project header.
func (p Provider) authenticate(ctx context.Context, out *http.Request, target gateway.Target, c Credentials) error {
	out.Header.Del("Authorization")
	out.Header.Del("X-Api-Key")
	out.Header.Del("X-Goog-Api-Key")
	out.Header.Del("Anthropic-Version")
	if c.APIKey != "" {
		query := out.URL.Query()
		query.Set("key", c.APIKey)
		out.URL.RawQuery = query.Encode()
	} else {
		token := c.AccessToken
		if token == "" {
			var err error
			token, err = p.accessToken(ctx, target.Secret, c, target.ProxyURL, target)
			if err != nil {
				_ = out.Body.Close()
				return err
			}
		}
		if strings.ContainsAny(token, "\r\n") {
			return errors.New("vertex: invalid access token")
		}
		out.Header.Set("Authorization", "Bearer "+token)
	}
	if c.ProjectID != "" {
		out.Header.Set("X-Goog-User-Project", c.ProjectID)
	}
	return nil
}

func rewriteClaudeBody(out *http.Request, stream bool) error {
	data, err := io.ReadAll(out.Body)
	_ = out.Body.Close()
	if err != nil {
		return err
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(data, &body) != nil || body == nil {
		return errors.New("vertex: Anthropic request must be a JSON object")
	}
	delete(body, "model")
	body["anthropic_version"] = json.RawMessage(`"vertex-2023-10-16"`)
	if stream {
		body["stream"] = json.RawMessage(`true`)
	} else {
		delete(body, "stream")
	}
	data, err = json.Marshal(body)
	if err != nil {
		return err
	}
	out.Body = io.NopCloser(bytes.NewReader(data))
	out.ContentLength = int64(len(data))
	out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	return nil
}

func (p Provider) Decode(req *gateway.Request, response *http.Response) gateway.EventStream {
	f := modelFamily(strings.TrimPrefix(req.Model, "models/"))
	// The actual request path is authoritative when aliases hide the model family.
	if response.Request != nil && response.Request.URL != nil {
		path := response.Request.URL.Path
		switch {
		case strings.Contains(path, "/publishers/anthropic/"):
			f = claude
		case strings.Contains(path, "/endpoints/openapi/"):
			f = openSource
		case strings.Contains(path, "/publishers/google/"):
			f = google
			if strings.HasSuffix(path, ":predict") {
				f = imagen
			}
		}
	}
	if f == imagen {
		return &imageStream{body: response.Body, req: req}
	}
	return p.adapter(f).Decode(req, response)
}

// BuildActionRequest authenticates already-converted Gemini media/vector JSON.
// The auxiliary handler owns media conversion and response decoding.
func (p Provider) BuildActionRequest(ctx context.Context, req *gateway.Request, target gateway.Target, action string) (*http.Request, error) {
	if req == nil || (action != "predict" && action != "embedContent" && action != "batchEmbedContents") {
		return nil, errors.New("vertex: unsupported native action")
	}
	copyReq := *req
	copyReq.Protocol, copyReq.Stream = gateway.ProtocolGemini, false
	out, err := p.BuildRequest(ctx, &copyReq, target)
	if err != nil {
		return nil, err
	}
	path := out.URL.Path
	colon := strings.LastIndex(path, ":")
	if colon < 0 {
		_ = out.Body.Close()
		return nil, errors.New("vertex: invalid native action model")
	}
	out.URL.Path = path[:colon+1] + action
	return out, nil
}
