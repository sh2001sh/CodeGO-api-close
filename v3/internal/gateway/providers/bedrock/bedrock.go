// Package bedrock adapts AWS Bedrock's signed model invocation API.
package bedrock

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/anthropic"
	"github.com/tidwall/sjson"
)

const ID = "bedrock"

type Provider struct {
	ImageTransport func(context.Context, gateway.Target) (http.RoundTripper, error)
}

func (p Provider) BuildRequest(ctx context.Context, req *gateway.Request, target gateway.Target) (*http.Request, error) {
	cred, err := credentials(target.Secret, target.BaseURL)
	if err != nil {
		return nil, err
	}
	model := target.UpstreamModel
	if model == "" {
		model = req.Model
	}
	model = modelID(model, cred.Region)
	if model == "" {
		return nil, fmt.Errorf("bedrock: model is required")
	}
	body, err := p.buildBody(ctx, req, target, model)
	if err != nil {
		return nil, err
	}
	action := "invoke"
	if req.Stream {
		action = "invoke-with-response-stream"
	}
	endpoint, err := endpointURL(target.BaseURL, cred.Region, model, action)
	if err != nil {
		return nil, err
	}
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("Accept", "application/json")
	if req.Stream {
		out.Header.Set("Accept", "application/vnd.amazon.eventstream")
		out.Header.Set("X-Amzn-Bedrock-Accept", "application/json")
	}
	if cred.Bearer != "" {
		out.Header.Set("Authorization", "Bearer "+cred.Bearer)
	} else {
		if err := signRequest(out, body, cred, time.Now()); err != nil {
			return nil, fmt.Errorf("bedrock: sign request: %w", err)
		}
	}
	return out, nil
}

// buildBody converts req to Bedrock's invocation body by delegating to the
// Anthropic Messages adapter, then rewriting the result for Bedrock
// (dropping client-only fields, inlining remote images, and converting to
// Nova's native shape when the model requires it).
func (p Provider) buildBody(ctx context.Context, req *gateway.Request, target gateway.Target, model string) ([]byte, error) {
	// Reuse the established Messages conversion; provider adapters still only
	// build HTTP requests and never execute a second transport themselves.
	convertedTarget := target
	convertedTarget.BaseURL = "https://bedrock.invalid"
	convertedTarget.UpstreamModel = model
	converted, err := (anthropic.Provider{}).BuildRequest(ctx, req, convertedTarget)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(converted.Body)
	_ = converted.Body.Close()
	if err != nil {
		return nil, err
	}
	body, err = sjson.DeleteBytes(body, "model")
	if err == nil {
		body, err = sjson.DeleteBytes(body, "stream")
	}
	if err == nil {
		body, err = sjson.SetBytes(body, "anthropic_version", "bedrock-2023-05-31")
	}
	if err != nil {
		return nil, err
	}
	body, err = p.inlineRemoteImages(ctx, body, target)
	if err != nil {
		return nil, err
	}
	if strings.Contains(model, "amazon.nova-") {
		body, err = novaRequest(body)
		if err != nil {
			return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "unsupported_request", Message: err.Error()}
		}
	}
	return body, nil
}

// endpointURL builds the Bedrock model-invocation URL, defaulting to the
// region's standard runtime host when target does not override it.
func endpointURL(baseURL, region, model, action string) (*url.URL, error) {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://bedrock-runtime." + region + ".amazonaws.com"
		if strings.HasPrefix(region, "cn-") {
			base += ".cn"
		}
	}
	endpoint, err := url.Parse(base)
	if err != nil || endpoint.Host == "" || endpoint.User != nil ||
		(endpoint.Scheme != "https" && endpoint.Scheme != "http") {
		return nil, fmt.Errorf("bedrock: base URL must be absolute HTTP")
	}
	escapedBase := strings.TrimRight(endpoint.EscapedPath(), "/")
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/model/" + model + "/" + action
	endpoint.RawPath = escapedBase + "/model/" + awsEncode(model, false) + "/" + action
	endpoint.Fragment = ""
	return endpoint, nil
}

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	copyResponse := *resp
	copyResponse.Header = resp.Header.Clone()
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/vnd.amazon.eventstream") {
		copyResponse.Body = newEventBody(resp.Body)
		copyResponse.Header.Set("Content-Type", "text/event-stream")
	} else {
		copyResponse.Body = &responseBody{body: resp.Body}
	}
	return (anthropic.Provider{}).Decode(req, &copyResponse)
}
