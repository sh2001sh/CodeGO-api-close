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
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers"
	"github.com/tidwall/sjson"
)

func probeBaseURL(target gateway.Target) string {
	if target.BaseURL != "" {
		return target.BaseURL
	}
	return map[string]string{
		"openai": "https://api.openai.com", "claude": "https://api.anthropic.com", "anthropic": "https://api.anthropic.com",
		"gemini": "https://generativelanguage.googleapis.com", "ollama": "http://localhost:11434", "codex": "https://chatgpt.com",
		"deepseek": "https://api.deepseek.com", "openrouter": "https://openrouter.ai/api", "moonshot": "https://api.moonshot.cn",
		"xai": "https://api.x.ai", "mistral": "https://api.mistral.ai", "siliconflow": "https://api.siliconflow.cn",
		"jina": "https://api.jina.ai", "tencent": "https://hunyuan.tencentcloudapi.com",
	}[target.Provider]
}

func probeRequest(model, endpoint string, stream bool) (*gateway.Request, error) {
	if model == "" || len(model) > 255 || strings.ContainsAny(model, "\r\n\x00") {
		return nil, errors.New("a valid probe model is required")
	}
	body := map[string]any{"model": model, "stream": stream, "max_tokens": 8,
		"messages": []any{map[string]string{"role": "user", "content": "Say OK."}}}
	protocol := gateway.ProtocolOpenAIChat
	switch endpoint {
	case "", "openai", "chat", "chat_completion", "chat_completions", "/v1/chat/completions":
	case "openai-response", "responses", "/v1/responses":
		protocol = gateway.ProtocolResponses
		delete(body, "messages")
		delete(body, "max_tokens")
		body["input"], body["max_output_tokens"] = "Say OK.", 16
	case "anthropic", "messages", "/v1/messages":
		protocol = gateway.ProtocolAnthropic
	case "gemini", "generate_content":
		protocol = gateway.ProtocolGemini
		body = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]string{"text": "Say OK."}}}}, "generationConfig": map[string]int{"maxOutputTokens": 8}}
	default:
		return nil, errors.New("unsupported probe endpoint")
	}
	data, err := json.Marshal(body)
	return &gateway.Request{Protocol: protocol, Model: model, Body: data, Stream: stream}, err
}

// buildProbeRequestBody resolves the probe target's base URL and builds the
// provider-agnostic probe request body, applying the Tencent max_tokens
// workaround.
func buildProbeRequestBody(target *gateway.Target, model, endpoint string, stream bool) (*gateway.Request, error) {
	target.BaseURL = probeBaseURL(*target)
	if target.BaseURL != "" {
		if _, err := validateProbeURL(target.BaseURL, false); err != nil {
			return nil, err
		}
	}
	request, err := probeRequest(model, endpoint, stream)
	if err != nil {
		return nil, err
	}
	// Tencent's native Chat API has no output-token limit option. A probe
	// must use supported native options instead of failing before transport.
	if target.Provider == "tencent" {
		request.Body, err = sjson.DeleteBytes(request.Body, "max_tokens")
		if err != nil {
			return nil, errors.New("probe request could not be built")
		}
	}
	return request, nil
}

// buildProbeUpstreamRequest builds and finalizes the upstream HTTP request
// for the probe, including the xunfei websocket-scheme validation fixup,
// fingerprint user-agent, channel overrides and request signing.
func buildProbeUpstreamRequest(ctx context.Context, adapter gateway.Provider, request *gateway.Request, target gateway.Target) (*http.Request, error) {
	upstream, err := adapter.BuildRequest(ctx, request, target)
	if err != nil {
		return nil, errors.New("probe request could not be built")
	}
	endpointURL := *upstream.URL
	if target.Provider == "xunfei" {
		if endpointURL.Scheme == "ws" {
			endpointURL.Scheme = "http"
		}
		if endpointURL.Scheme == "wss" {
			endpointURL.Scheme = "https"
		}
	}
	if _, err = validateProbeURL(endpointURL.String(), false); err != nil {
		return nil, err
	}
	if target.Fingerprint.UserAgent != "" {
		upstream.Header.Set("User-Agent", target.Fingerprint.UserAgent)
	}
	if err := gateway.ApplyUpstreamRequest(upstream, request, target); err != nil {
		return nil, errors.New("probe channel overrides could not be applied")
	}
	if finalizer, ok := adapter.(gateway.RequestFinalizer); ok {
		if err := finalizer.FinalizeRequest(ctx, upstream, request, target); err != nil {
			return nil, errors.New("probe final request could not be signed")
		}
	}
	return upstream, nil
}

// sendProbeRequest dispatches the upstream probe request and returns a
// response whose body has been fully buffered (subject to probeBodyLimit)
// and status-mapped, ready for event decoding.
func sendProbeRequest(adapter gateway.Provider, request *gateway.Request, target gateway.Target, upstream *http.Request) (*http.Response, error) {
	client, closeIdle, err := probeClient(target.ProxyURL)
	if err != nil {
		return nil, err
	}
	defer closeIdle()
	if selector, ok := adapter.(gateway.TransportProvider); ok {
		client.Transport = selector.UpstreamTransport(request, client.Transport)
	}
	response, err := client.Do(upstream)
	if err != nil {
		return nil, errors.New("upstream probe failed or timed out")
	}
	originalBody := response.Body
	defer func() { _ = originalBody.Close() }()
	response.StatusCode = gateway.MapUpstreamStatus(response.StatusCode, target)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, errors.New("upstream rejected the probe")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, probeBodyLimit+1))
	if err != nil || len(data) > probeBodyLimit {
		return nil, errors.New("invalid, truncated or oversized upstream response")
	}
	response.Body = io.NopCloser(bytes.NewReader(data))
	return response, nil
}

// decodeProbeEvents consumes the decoded event stream from response and
// reports whether text content and a completion event were observed.
func decodeProbeEvents(adapter gateway.Provider, request *gateway.Request, response *http.Response) (text, done bool, err error) {
	events := adapter.Decode(request, response)
	defer func() { _ = events.Close() }()
	for {
		event, e := events.Next()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil || event.Kind == gateway.EventError {
			return false, false, errors.New("upstream probe returned an invalid response or an error")
		}
		text = text || event.TextBytes > 0
		done = done || event.Kind == gateway.EventDone
	}
	return text, done, nil
}

func runChannelProbe(parent context.Context, target gateway.Target, model, endpoint string, stream bool) error {
	if operation, ok := auxProbeOperation(endpoint); ok {
		return runAuxChannelProbe(parent, target, model, operation, stream)
	}
	adapter := providers.Registry()[target.Provider]
	if adapter == nil {
		return errors.New("unsupported channel provider")
	}
	request, err := buildProbeRequestBody(&target, model, endpoint, stream)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, probeTimeout)
	defer cancel()
	upstream, err := buildProbeUpstreamRequest(ctx, adapter, request, target)
	if err != nil {
		return err
	}
	defer func() {
		if upstream.Body != nil {
			_ = upstream.Body.Close()
		}
	}()
	response, err := sendProbeRequest(adapter, request, target, upstream)
	if err != nil {
		return err
	}
	text, done, err := decodeProbeEvents(adapter, request, response)
	if err != nil {
		return err
	}
	if !text || (stream && !done) {
		return errors.New("upstream probe did not return completed content")
	}
	return nil
}
