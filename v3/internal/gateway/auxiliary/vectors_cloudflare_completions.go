package auxiliary

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/cloudflare"
	"github.com/tidwall/gjson"
)

func buildCloudflareCompletions(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	fields, err := vectorFields(in.Body, "model", "prompt", "max_tokens", "max_completion_tokens", "stream", "stream_options", "temperature", "n", "echo")
	if err != nil {
		return nil, err
	}
	if err := validateCloudflareCompletionFlags(fields); err != nil {
		return nil, err
	}
	payload, err := cloudflareCompletionPayload(req, fields)
	if err != nil {
		return nil, err
	}
	model, err := cloudflareCompletionModel(req, target)
	if err != nil {
		return nil, err
	}
	base, secret, err := vectorCloudflareBase(target.BaseURL, target.Secret)
	if err != nil {
		return nil, err
	}
	address, err := endpoint(base, "/run/"+model)
	if err != nil {
		return nil, err
	}
	wire, err := jsonRequest(ctx, address, secret, payload)
	if err == nil && req.Stream {
		wire.Header.Set("Accept", "text/event-stream")
	}
	return wire, err
}

// validateCloudflareCompletionFlags rejects completion fields that
// Cloudflare's text-completion endpoint cannot honor (n != 1, echo, or
// malformed stream/stream_options).
func validateCloudflareCompletionFlags(fields map[string]json.RawMessage) error {
	if raw := fields["n"]; len(raw) > 0 {
		var n int
		if json.Unmarshal(raw, &n) != nil || n != 1 {
			return vectorInvalid("Cloudflare completions supports n=1 only")
		}
	}
	if raw := fields["echo"]; len(raw) > 0 {
		var echo bool
		if json.Unmarshal(raw, &echo) != nil || echo {
			return vectorInvalid("Cloudflare completions does not support echo")
		}
	}
	if raw := fields["stream"]; len(raw) > 0 {
		var stream bool
		if json.Unmarshal(raw, &stream) != nil {
			return vectorInvalid("stream must be boolean")
		}
	}
	if raw := fields["stream_options"]; len(raw) > 0 {
		var options map[string]json.RawMessage
		if json.Unmarshal(raw, &options) != nil || options == nil {
			return vectorInvalid("stream_options must be an object")
		}
		for name, value := range options {
			var enabled bool
			if name != "include_usage" || json.Unmarshal(value, &enabled) != nil {
				return vectorInvalid("unsupported completion stream option")
			}
		}
	}
	return nil
}

// cloudflareCompletionPayload builds the upstream JSON payload from the
// client's validated completion fields.
func cloudflareCompletionPayload(req *gateway.Request, fields map[string]json.RawMessage) (map[string]any, error) {
	var prompt string
	if json.Unmarshal(fields["prompt"], &prompt) != nil || prompt == "" {
		return nil, vectorInvalid("Cloudflare completions requires a nonempty text prompt")
	}
	var maxTokens uint64
	for _, name := range []string{"max_tokens", "max_completion_tokens"} {
		if raw := fields[name]; len(raw) > 0 {
			var value uint64
			if json.Unmarshal(raw, &value) != nil {
				return nil, vectorInvalid("max tokens must be a nonnegative integer")
			}
			if name == "max_tokens" || value > 0 {
				maxTokens = value
			}
		}
	}
	payload := map[string]any{"prompt": prompt, "max_tokens": maxTokens, "stream": req.Stream}
	if raw := fields["temperature"]; len(raw) > 0 {
		var temperature float64
		if json.Unmarshal(raw, &temperature) != nil || temperature < 0 {
			return nil, vectorInvalid("temperature must be nonnegative numeric")
		}
		payload["temperature"] = temperature
	}
	return payload, nil
}

// cloudflareCompletionModel resolves and validates the upstream model path
// segment, rejecting anything that could escape the /run/{model} route.
func cloudflareCompletionModel(req *gateway.Request, target gateway.Target) (string, error) {
	model := upstreamModel(req, target)
	if model == "" || strings.ContainsAny(model, "\\?#%") {
		return "", vectorInvalid("invalid Cloudflare completion model")
	}
	for _, segment := range strings.Split(model, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", vectorInvalid("invalid Cloudflare completion model")
		}
	}
	return model, nil
}

func decodeCloudflareCompletions(req *gateway.Request, resp *http.Response) (Response, error) {
	if req.Stream {
		_ = resp.Body.Close()
		return Response{}, vectorUpstream("invalid_response", "Cloudflare returned JSON instead of a completion stream")
	}
	copyReq := *req
	copyReq.Stream = false
	stream := newCloudflareCompletionStream(&copyReq, resp)
	defer func() { _ = stream.Close() }()
	event, err := stream.Next()
	if err != nil {
		return Response{}, err
	}
	if event.Kind == gateway.EventError {
		return Response{}, event.Err
	}
	if event.Kind != gateway.EventData {
		return Response{}, vectorUpstream("invalid_response", "Cloudflare returned no completion")
	}
	return Response{Body: event.Payload, Header: vectorJSONHeaders(resp.Header), Usage: event.Usage}, nil
}

// DecodeStream is optional to the auxiliary adapter contract. Nil leaves an
// unrelated operation with its ordinary decoder; native completions must be
// consumed as these converted events rather than forwarding upstream SSE.
func (a vectorAdapter) DecodeStream(req *gateway.Request, _ gateway.Target, in Input, resp *http.Response) gateway.EventStream {
	if a.provider != "cloudflare" || in.Operation != Completions {
		return nil
	}
	copyReq := *req
	copyReq.Stream = true
	return newCloudflareCompletionStream(&copyReq, resp)
}

type cloudflareCompletionStream struct {
	source gateway.EventStream
	id     string
}

func newCloudflareCompletionStream(req *gateway.Request, resp *http.Response) gateway.EventStream {
	return &cloudflareCompletionStream{source: cloudflare.Provider{}.Decode(req, resp), id: "cmpl-" + req.ID}
}

func (s *cloudflareCompletionStream) Close() error { return s.source.Close() }

func (s *cloudflareCompletionStream) Next() (gateway.Event, error) {
	event, err := s.source.Next()
	if err != nil || event.Kind != gateway.EventData {
		return event, err
	}
	fields, err := object(event.Payload)
	if err != nil {
		return gateway.Event{}, vectorUpstream("invalid_response", "invalid native completion event")
	}
	var choices []map[string]json.RawMessage
	if json.Unmarshal(fields["choices"], &choices) != nil {
		return gateway.Event{}, vectorUpstream("invalid_response", "invalid native completion choices")
	}
	for _, choice := range choices {
		root := gjson.ParseBytes(choice["delta"])
		if !root.IsObject() {
			root = gjson.ParseBytes(choice["message"])
		}
		choice["text"], _ = json.Marshal(root.Get("content").String())
		choice["logprobs"] = json.RawMessage("null")
		delete(choice, "delta")
		delete(choice, "message")
	}
	fields["choices"], _ = json.Marshal(choices)
	fields["id"], _ = json.Marshal(s.id)
	fields["object"] = json.RawMessage(`"text_completion"`)
	event.Payload, err = json.Marshal(fields)
	return event, err
}
