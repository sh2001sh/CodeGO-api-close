package gateway

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
)

// BuildProviderRequest preserves explicit raw forwarding without allowing a
// native converter to reject, rewrite or download fields in the original body.
// The template is used only to obtain the provider endpoint and authentication;
// it is replaced before dispatch, overrides and final-byte signing.
func BuildProviderRequest(ctx context.Context, provider Provider, req *Request, target Target) (*http.Request, error) {
	pass, err := overrideSettingBool(target.Settings, "pass_through_body_enabled")
	if err != nil {
		return nil, invalidOverride("invalid_channel_settings")
	}
	if !pass {
		prepared := *req
		// Disabled source fields must be removed before a native converter can
		// reject them. System prompts are injected into the converted schema.
		settings := maps.Clone(target.Settings)
		delete(settings, "system_prompt")
		delete(settings, "system_prompt_override")
		prepared.Body, err = applyOverrideSettings(req.Body, settings, req.Protocol == ProtocolAnthropic)
		if err != nil {
			return nil, invalidOverride("invalid_channel_settings")
		}
		return provider.BuildRequest(ctx, &prepared, target)
	}
	if wrapper, ok := provider.(RawRequestBuilder); ok {
		return wrapper.BuildRawRequest(ctx, req, target)
	}
	prepared := *req
	prepared.Body, err = rawEndpointTemplate(req)
	if err != nil {
		return nil, err
	}
	out, err := provider.BuildRequest(ctx, &prepared, target)
	if err != nil {
		return nil, err
	}
	closeRequestBody(out)
	resetOverrideBody(out, req.Body)
	return out, nil
}

func rawEndpointTemplate(req *Request) ([]byte, error) {
	body := map[string]any{"model": req.Model, "stream": req.Stream}
	message := map[string]any{"role": "user", "content": "endpoint template"}
	switch req.Protocol {
	case ProtocolOpenAIChat:
		body["messages"] = []any{message}
	case ProtocolResponses:
		body["input"] = "endpoint template"
	case ProtocolAnthropic:
		body["messages"], body["max_tokens"] = []any{message}, 1
	case ProtocolGemini:
		body["contents"] = []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "endpoint template"}}}}
	default:
		return nil, invalidOverride("unsupported_raw_protocol")
	}
	return json.Marshal(body)
}
