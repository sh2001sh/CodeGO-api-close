// Package azure adapts Azure OpenAI deployment and Responses endpoints.
package azure

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
)

const ID = "azure"

type Provider struct{}

func (Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	endpoint, err := url.Parse(target.BaseURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") {
		return nil, errors.New("azure: base URL must be an absolute HTTP URL")
	}
	delegate, version, err := routeProtocol(req, target, endpoint)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if query.Get("api-version") == "" {
		query.Set("api-version", version)
	}
	endpoint.RawQuery = query.Encode()
	endpoint.RawPath = ""
	// Build body and stream usage through the canonical protocol adapter, then
	// replace only its endpoint and authentication.
	canonical := target
	canonical.BaseURL = "https://azure.invalid"
	out, err := delegate.BuildRequest(ctx, req, canonical)
	if err != nil {
		return nil, err
	}
	out.URL = endpoint
	out.Host = ""
	out.Header.Del("Authorization")
	out.Header.Set("Api-Key", target.Secret)
	return out, nil
}

// routeProtocol selects the canonical adapter for req's client protocol,
// rewrites endpoint.Path to Azure's deployment/Responses path shape, and
// resolves the API version to use. endpoint is mutated in place.
func routeProtocol(req *gateway.Request, target gateway.Target, endpoint *url.URL) (gateway.Provider, string, error) {
	version := "2024-10-21"
	configured, err := settingVersion(target.Settings, "api_version")
	if err != nil {
		return nil, "", err
	}
	if configured != "" {
		version = configured
	}
	model := target.UpstreamModel
	if model == "" {
		model = req.Model
	}
	switch req.Protocol {
	case gateway.ProtocolOpenAIChat:
		if model == "" {
			return nil, "", errors.New("azure: deployment is required")
		}
		endpoint.Path = chatPath(endpoint.Path, model)
		return openai.Provider{}, version, nil
	case gateway.ProtocolResponses:
		endpoint.Path = responsesPath(endpoint.Path, endpoint.Hostname())
		// V2 uses the channel API version for Cognitive Services, while the
		// /openai/v1 Responses endpoint defaults independently to preview.
		if !strings.HasSuffix(endpoint.Hostname(), ".cognitiveservices.azure.com") {
			version = "preview"
		}
		responsesVersion, err := settingVersion(target.Settings, "azure_responses_version")
		if err != nil {
			return nil, "", err
		}
		if responsesVersion != "" {
			version = responsesVersion
		}
		return responses.Provider{}, version, nil
	default:
		return nil, "", &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "unsupported_protocol",
			Message: fmt.Sprintf("azure: unsupported client protocol %d", req.Protocol)}
	}
}

func settingVersion(settings map[string]any, name string) (string, error) {
	value, exists := settings[name]
	if !exists || value == nil {
		return "", nil
	}
	version, ok := value.(string)
	if !ok || strings.ContainsAny(version, "\r\n") {
		return "", &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: "invalid_channel_api_version",
			Message: "azure: invalid channel API version"}
	}
	return strings.TrimSpace(version), nil
}

func chatPath(base, model string) string {
	base = strings.TrimRight(base, "/")
	base = strings.TrimSuffix(base, "/chat/completions")
	if i := strings.LastIndex(base, "/deployments/"); i >= 0 {
		return base[:i] + "/deployments/" + model + "/chat/completions"
	}
	base = strings.TrimSuffix(base, "/v1")
	if !strings.HasSuffix(base, "/openai") {
		base += "/openai"
	}
	return base + "/deployments/" + model + "/chat/completions"
}

func responsesPath(base, host string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/responses") {
		return base
	}
	base = strings.TrimSuffix(base, "/v1")
	if !strings.HasSuffix(base, "/openai") {
		base += "/openai"
	}
	if strings.HasSuffix(host, ".cognitiveservices.azure.com") {
		return base + "/responses"
	}
	return base + "/v1/responses"
}

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	if req.Protocol == gateway.ProtocolResponses {
		return (responses.Provider{}).Decode(req, resp)
	}
	return (openai.Provider{}).Decode(req, resp)
}
