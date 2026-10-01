// Package replicate adapts native image prediction jobs. Replicate's v2 channel
// supports image generation and edits, rather than the gateway's Chat protocols.
package replicate

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const ID = "replicate"
const maxJSONBody = 64 << 20

// Provider's zero value uses the standard HTTP transport. Client can supply a
// custom transport; PollInterval defaults to 500 milliseconds.
type Provider struct {
	Client       *http.Client
	PollInterval time.Duration
	injected     bool
}

type transportOptions struct {
	pollPath string
	proxy    *url.URL
}

type optionsKey struct{}

func (Provider) BuildRequest(context.Context, *gateway.Request, gateway.Target) (*http.Request, error) {
	return nil, invalid("unsupported_protocol", "replicate supports image generation and image edits only")
}

func (Provider) Decode(_ *gateway.Request, resp *http.Response) gateway.EventStream {
	return &unsupportedStream{body: resp.Body}
}

// BuildImageRequest accepts JSON image generation, or edit requests whose
// image_prompt has already been uploaded by the auxiliary endpoint handler.
func BuildImageRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	if req == nil {
		return nil, invalid("invalid_request", "replicate image request is missing")
	}
	if strings.TrimSpace(target.Secret) == "" {
		return nil, upstream("invalid_credentials", "replicate channel requires an API key")
	}
	model := target.UpstreamModel
	if model == "" {
		model = req.Model
	}
	if model == "" {
		model = "black-forest-labs/flux-1.1-pro"
	}
	path, version, err := predictionEndpoint(model)
	if err != nil {
		if target.UpstreamModel != "" {
			return nil, upstream("invalid_model", "replicate channel has an invalid upstream model selector")
		}
		return nil, err
	}
	body, err := convertImageRequest(req.Body, version)
	if err != nil {
		return nil, err
	}
	base := target.BaseURL
	if base == "" {
		base = "https://api.replicate.com"
	}
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, upstream("invalid_endpoint", "replicate channel endpoint must be an HTTP base URL")
	}
	prefix := strings.TrimSuffix(u.Path, "/v1")
	opts := transportOptions{pollPath: prefix + "/v1/predictions/"}
	if target.ProxyURL != "" {
		opts.proxy, err = url.Parse(target.ProxyURL)
		if err != nil || opts.proxy.Host == "" || (opts.proxy.Scheme != "http" && opts.proxy.Scheme != "https" && opts.proxy.Scheme != "socks5" && opts.proxy.Scheme != "socks5h") {
			return nil, upstream("invalid_proxy", "replicate channel proxy URL is invalid")
		}
	}
	u.Path, u.RawPath = prefix+path, ""
	out, err := http.NewRequestWithContext(context.WithValue(ctx, optionsKey{}, opts), http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Authorization", "Bearer "+target.Secret)
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("Accept", "application/json")
	out.Header.Set("Prefer", "wait")
	return out, nil
}

func predictionEndpoint(model string) (string, string, error) {
	name, version, pinned := strings.Cut(strings.TrimSpace(model), ":")
	parts := strings.Split(name, "/")
	if len(parts) == 2 && validName(parts[0]) && validName(parts[1]) {
		if !pinned {
			return "/v1/models/" + name + "/predictions", "", nil
		}
		if validID(version) {
			return "/v1/predictions", version, nil
		}
	} else if !pinned && validID(name) {
		return "/v1/predictions", name, nil
	}
	return "", "", invalid("invalid_model", "replicate model must be owner/model, owner/model:version, or a version ID")
}

func validName(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

func validID(s string) bool {
	return validName(s) && !strings.Contains(s, ".")
}

func invalid(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: code, Message: message}
}

func upstream(code, message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: code, Message: message}
}

// Keep native input values as JSON rather than converting large integers to floats.
func object(data []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return nil, invalid("invalid_json", "replicate request must be a JSON object")
	}
	return fields, nil
}
