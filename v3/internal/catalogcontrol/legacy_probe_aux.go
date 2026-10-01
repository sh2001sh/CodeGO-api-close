package catalogcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/auxiliary"
	"github.com/tidwall/gjson"
)

func auxProbeOperation(endpoint string) (auxiliary.Operation, bool) {
	switch endpoint {
	case "embeddings", "embedding", "openai-embedding", "/v1/embeddings":
		return auxiliary.Embeddings, true
	case "jina-rerank", "rerank", "/v1/rerank":
		return auxiliary.Rerank, true
	case "image-generation", "images/generations", "/v1/images/generations":
		return auxiliary.Images, true
	case "openai-response-compact", "responses/compact", "/v1/responses/compact":
		return auxiliary.Compact, true
	}
	return "", false
}

// buildAuxProbeInput resolves the probe target's base URL and builds the
// operation-specific request body and auxiliary.Input for the probe.
func buildAuxProbeInput(target *gateway.Target, model string, operation auxiliary.Operation) (*gateway.Request, auxiliary.Input, error) {
	target.BaseURL = probeBaseURL(*target)
	if target.BaseURL != "" {
		if _, err := validateProbeURL(target.BaseURL, false); err != nil {
			return nil, auxiliary.Input{}, err
		}
	}
	body := map[string]any{"model": model}
	switch operation {
	case auxiliary.Embeddings:
		body["input"] = []string{"probe"}
	case auxiliary.Rerank:
		body["query"], body["documents"], body["top_n"] = "probe", []string{"probe"}, 1
	case auxiliary.Images:
		body["prompt"], body["n"], body["size"] = "A simple blue square.", 1, "1024x1024"
	case auxiliary.Compact:
		body["input"] = []any{map[string]string{"role": "user", "content": "OK"}}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, auxiliary.Input{}, err
	}
	request := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: model, Body: raw}
	input := auxiliary.Input{Operation: operation, Path: "/v1/" + string(operation), ContentType: "application/json", Body: raw}
	return request, input, nil
}

// buildAuxUpstreamRequest builds and finalizes the upstream HTTP request for
// the auxiliary probe: fingerprint user-agent, channel overrides and request
// signing.
func buildAuxUpstreamRequest(ctx context.Context, adapter auxiliary.Adapter, request *gateway.Request, target gateway.Target, input auxiliary.Input) (*http.Request, error) {
	upstream, err := adapter.Build(ctx, request, target, input)
	if err != nil {
		return nil, errors.New("auxiliary probe request could not be built")
	}
	if _, err := validateProbeURL(upstream.URL.String(), false); err != nil {
		return nil, err
	}
	if target.Fingerprint.UserAgent != "" {
		upstream.Header.Set("User-Agent", target.Fingerprint.UserAgent)
	}
	if err := gateway.ApplyUpstreamRequest(upstream, request, target); err != nil {
		return nil, errors.New("auxiliary probe overrides could not be applied")
	}
	if finalizer, ok := adapter.(gateway.RequestFinalizer); ok {
		if err := finalizer.FinalizeRequest(ctx, upstream, request, target); err != nil {
			return nil, errors.New("auxiliary probe final request could not be signed")
		}
	}
	return upstream, nil
}

// sendAuxProbeRequest dispatches the upstream auxiliary probe request and
// returns a response whose body has been fully buffered (subject to
// probeBodyLimit) and status-mapped.
func sendAuxProbeRequest(target gateway.Target, upstream *http.Request) (*http.Response, error) {
	client, closeIdle, err := probeClient(target.ProxyURL)
	if err != nil {
		return nil, err
	}
	defer closeIdle()
	response, err := client.Do(upstream)
	if err != nil {
		return nil, errors.New("auxiliary probe failed or timed out")
	}
	original := response.Body
	defer func() { _ = original.Close() }()
	response.StatusCode = gateway.MapUpstreamStatus(response.StatusCode, target)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, errors.New("upstream rejected auxiliary probe")
	}
	data, err := io.ReadAll(io.LimitReader(original, probeBodyLimit+1))
	if err != nil || len(data) > probeBodyLimit {
		return nil, errors.New("invalid or oversized auxiliary probe response")
	}
	response.Body = io.NopCloser(bytes.NewReader(data))
	return response, nil
}

// auxProbeResultKey returns the result JSON key expected for operation.
func auxProbeResultKey(operation auxiliary.Operation) string {
	switch operation {
	case auxiliary.Rerank:
		return "results"
	case auxiliary.Compact:
		return "output"
	default:
		return "data"
	}
}

func runAuxChannelProbe(parent context.Context, target gateway.Target, model string, operation auxiliary.Operation, stream bool) error {
	if stream || model == "" || len(model) > 255 || strings.ContainsAny(model, "\r\n\x00") {
		return errors.New("auxiliary probes require a model and a non-stream response")
	}
	adapter := auxiliary.AdapterFor(target.Provider)
	if adapter == nil {
		return errors.New("unsupported auxiliary probe provider")
	}
	request, input, err := buildAuxProbeInput(&target, model, operation)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, probeTimeout)
	defer cancel()
	upstream, err := buildAuxUpstreamRequest(ctx, adapter, request, target, input)
	if err != nil {
		return err
	}
	defer func() {
		if upstream.Body != nil {
			_ = upstream.Body.Close()
		}
	}()
	response, err := sendAuxProbeRequest(target, upstream)
	if err != nil {
		return err
	}
	result, err := adapter.Decode(ctx, request, target, input, response)
	if err != nil {
		return errors.New("invalid auxiliary probe response")
	}
	items := gjson.GetBytes(result.Body, auxProbeResultKey(operation))
	if !items.IsArray() || len(items.Array()) == 0 {
		return errors.New("auxiliary probe did not return usable output")
	}
	return nil
}
