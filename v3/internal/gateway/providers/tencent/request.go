package tencent

import (
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/tidwall/gjson"
)

type nativeMessage struct {
	Role      string `json:"Role"`
	Content   string `json:"Content"`
	Reasoning string `json:"ReasoningContent,omitempty"`
}

type nativeRequest struct {
	Model       string          `json:"Model"`
	Messages    []nativeMessage `json:"Messages"`
	Stream      bool            `json:"Stream"`
	TopP        *float64        `json:"TopP,omitempty"`
	Temperature *float64        `json:"Temperature,omitempty"`
}

func convertRequest(body []byte, model string, stream bool) ([]byte, error) {
	root := gjson.ParseBytes(body)
	if !gjson.ValidBytes(body) || !root.IsObject() || model == "" {
		return nil, errors.New("tencent: invalid chat request")
	}
	// Native legacy Chat does not implement these OpenAI features. Reject them
	// explicitly rather than signing a request that silently loses their meaning.
	for _, field := range []string{"tools", "tool_choice", "functions", "function_call", "response_format", "stop", "max_tokens", "max_completion_tokens", "logprobs", "top_logprobs", "logit_bias", "frequency_penalty", "presence_penalty", "seed", "modalities", "audio", "prediction"} {
		if value := root.Get(field); value.Exists() && value.Type != gjson.Null {
			return nil, errors.New("tencent: unsupported chat option " + field)
		}
	}
	if n := root.Get("n"); n.Exists() && (n.Type != gjson.Number || n.Raw != "1") {
		return nil, errors.New("tencent: only one completion is supported")
	}
	messages := root.Get("messages")
	if !messages.IsArray() || len(messages.Array()) == 0 {
		return nil, errors.New("tencent: messages must be a nonempty array")
	}
	out := nativeRequest{Model: model, Stream: stream}
	for _, message := range messages.Array() {
		role := message.Get("role").Str
		if !message.IsObject() || (role != "user" && role != "assistant" && role != "system") || message.Get("tool_calls").Exists() || message.Get("function_call").Exists() {
			return nil, errors.New("tencent: unsupported chat message")
		}
		content := message.Get("content")
		text := content.Str
		if content.IsArray() {
			var parts strings.Builder
			for _, part := range content.Array() {
				if !part.IsObject() || part.Get("type").Str != "text" || part.Get("text").Type != gjson.String {
					return nil, errors.New("tencent: native legacy chat supports text content only")
				}
				parts.WriteString(part.Get("text").Str)
			}
			text = parts.String()
		} else if content.Type != gjson.String {
			return nil, errors.New("tencent: message content must contain text")
		}
		out.Messages = append(out.Messages, nativeMessage{Role: role, Content: text})
	}
	for _, option := range []struct {
		name string
		dst  **float64
		max  float64
	}{{"temperature", &out.Temperature, 2}, {"top_p", &out.TopP, 1}} {
		if value := root.Get(option.name); value.Exists() && value.Type != gjson.Null {
			number := value.Float()
			if value.Type != gjson.Number || math.IsInf(number, 0) || math.IsNaN(number) || number < 0 || number > option.max {
				return nil, errors.New("tencent: invalid sampling option " + option.name)
			}
			*option.dst = &number
		}
	}
	return json.Marshal(out)
}
