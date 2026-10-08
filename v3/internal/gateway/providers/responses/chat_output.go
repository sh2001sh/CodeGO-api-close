package responses

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type chatTool struct {
	Index    int            `json:"index"`
	ID       string         `json:"id,omitempty"`
	Type     string         `json:"type,omitempty"`
	Function map[string]any `json:"function"`
}

func chatOutputError() *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: "unsupported_upstream_response", Message: "Responses output cannot be represented as Chat"}
}

func chatUsage(root gjson.Result, usage *gateway.Usage) map[string]any {
	if usage == nil {
		return nil
	}
	out := map[string]any{"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens, "total_tokens": usage.PromptTokens + usage.CompletionTokens}
	if details := root.Get("input_tokens_details"); details.IsObject() {
		out["prompt_tokens_details"] = json.RawMessage(details.Raw)
	} else if usage.CachedTokens > 0 {
		out["prompt_tokens_details"] = map[string]any{"cached_tokens": usage.CachedTokens}
	}
	if details := root.Get("output_tokens_details"); details.IsObject() {
		out["completion_tokens_details"] = json.RawMessage(details.Raw)
	}
	return out
}

func chatFinish(root gjson.Result, tools bool) string {
	switch root.Get("incomplete_details.reason").Str {
	case "max_output_tokens":
		return "length"
	case "content_filter":
		return "content_filter"
	}
	if root.Get("status").Str == "incomplete" {
		return "length"
	}
	if tools {
		return "tool_calls"
	}
	return "stop"
}

func chatMessage(root gjson.Result) (map[string]any, bool, *gateway.UpstreamError) {
	var text, reasoning, refusal strings.Builder
	var tools []map[string]any
	for _, item := range root.Get("output").Array() {
		switch item.Get("type").Str {
		case "message":
			for _, part := range item.Get("content").Array() {
				switch part.Get("type").Str {
				case "output_text":
					text.WriteString(part.Get("text").Str)
				case "refusal":
					refusal.WriteString(part.Get("refusal").Str)
				default:
					return nil, false, chatOutputError()
				}
			}
		case "reasoning":
			for _, path := range []string{"summary", "content"} {
				for _, part := range item.Get(path).Array() {
					if !part.Get("text").Exists() {
						return nil, false, chatOutputError()
					}
					reasoning.WriteString(part.Get("text").Str)
				}
			}
		case "function_call":
			if item.Get("call_id").Str == "" || item.Get("name").Str == "" {
				return nil, false, chatOutputError()
			}
			tools = append(tools, map[string]any{"id": item.Get("call_id").Str, "type": "function", "function": map[string]any{"name": item.Get("name").Str, "arguments": item.Get("arguments").Str}})
		default:
			return nil, false, chatOutputError()
		}
	}
	message := map[string]any{"role": "assistant", "content": text.String()}
	if len(tools) > 0 {
		message["tool_calls"] = tools
		if text.Len() == 0 {
			message["content"] = nil
		}
	}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if refusal.Len() > 0 {
		message["refusal"] = refusal.String()
	}
	return message, len(tools) > 0, nil
}

func (s *chatStream) envelope(object string, choices any) map[string]any {
	out := map[string]any{"id": s.id, "object": object, "created": s.created, "model": s.model, "choices": choices}
	if s.serviceTier != "" {
		out["service_tier"] = s.serviceTier
	}
	return out
}

func (s *chatStream) updateMetadata(root gjson.Result) {
	if tier := root.Get("service_tier").Str; tier != "" {
		s.serviceTier = tier
	}
	if s.started {
		return
	}
	if id := root.Get("id").Str; id != "" {
		s.id = id
	}
	if model := root.Get("model").Str; model != "" {
		s.model = model
	}
	if created := root.Get("created_at"); created.Exists() {
		s.created = created.Int()
	}
}

func (s *chatStream) complete(ev gateway.Event, root gjson.Result) gateway.Event {
	s.updateMetadata(root)
	message, tools, failure := chatMessage(root)
	if failure != nil {
		return gateway.Event{Kind: gateway.EventError, Err: failure, Usage: ev.Usage, ServiceTier: ev.ServiceTier}
	}
	out := s.envelope("chat.completion", []any{map[string]any{"index": 0, "message": message, "finish_reason": chatFinish(root, tools)}})
	if usage := chatUsage(root.Get("usage"), ev.Usage); usage != nil {
		out["usage"] = usage
	}
	ev.Name = ""
	ev.Payload, _ = json.Marshal(out)
	return ev
}

func (s *chatStream) chunk(delta map[string]any, finish any, textBytes int) gateway.Event {
	if !s.started && len(delta) > 0 {
		delta["role"] = "assistant"
		s.started = true
	}
	out := s.envelope("chat.completion.chunk", []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}})
	body, _ := json.Marshal(out)
	return gateway.Event{Kind: gateway.EventData, Payload: body, TextBytes: textBytes}
}
