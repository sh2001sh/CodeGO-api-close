package auxiliary

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/vertex"
)

type vertexNativeAdapter struct{}

func vertexAdapter() Adapter { return vertexNativeAdapter{} }

func (vertexNativeAdapter) Build(ctx context.Context, req *gateway.Request, target gateway.Target, input Input) (*http.Request, error) {
	if req == nil {
		return nil, geminiInvalid("missing Vertex request")
	}
	// Conversion is local. Vertex credentials never enter the temporary Google
	// request, and its URL is never contacted.
	conversionTarget := target
	conversionTarget.BaseURL, conversionTarget.Secret = "https://gemini.invalid", ""
	converted, err := geminiAdapter().Build(ctx, req, conversionTarget, input)
	if err != nil {
		return nil, err
	}
	defer func() { _ = converted.Body.Close() }()
	body, err := io.ReadAll(converted.Body)
	if err != nil {
		return nil, err
	}
	_, action, ok := strings.Cut(converted.URL.Path, ":")
	if !ok {
		return nil, geminiInvalid("invalid Vertex native action")
	}
	native := *req
	native.Body, native.Protocol, native.Stream = body, gateway.ProtocolGemini, false
	provider := vertex.Provider{
		Clients: vertexSelectedClient,
		ImageTransport: func(ctx context.Context, selected gateway.Target) (http.RoundTripper, error) {
			client, err := vertexSelectedClient(ctx, selected)
			if err != nil {
				return nil, err
			}
			return client.Transport, nil
		},
	}
	return provider.BuildActionRequest(ctx, &native, target, action)
}

func vertexSelectedClient(ctx context.Context, _ gateway.Target) (*http.Client, error) {
	if client, ok := ctx.Value(clientKey{}).(*http.Client); ok && client != nil {
		return upstreamClient(ctx), nil
	}
	return nil, &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: "invalid_channel_configuration", Message: "selected Vertex client is unavailable"}
}

func (vertexNativeAdapter) Decode(ctx context.Context, req *gateway.Request, target gateway.Target, input Input, response *http.Response) (Response, error) {
	return geminiAdapter().Decode(ctx, req, target, input, response)
}
