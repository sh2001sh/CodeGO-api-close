package cloudflare

import (
	"encoding/json"
	"fmt"
	"math"
)

func convertRequest(data []byte, stream bool) ([]byte, error) {
	var in map[string]json.RawMessage
	if err := json.Unmarshal(data, &in); err != nil || in == nil {
		return nil, fmt.Errorf("cloudflare: Chat body must be a JSON object")
	}
	out := map[string]any{"stream": stream}
	for field, raw := range in {
		if string(raw) == "null" {
			continue
		}
		if err := convertField(field, raw, out); err != nil {
			return nil, err
		}
	}
	// Completion-token limits take precedence, as in the Chat API.
	if raw := in["max_completion_tokens"]; len(raw) != 0 && string(raw) != "null" {
		out["max_tokens"] = raw
	}
	messages, err := convertMessages(in["messages"])
	if err != nil {
		return nil, err
	}
	out["messages"] = messages
	return json.Marshal(out)
}

// convertField validates a single top-level request field and, if the
// Cloudflare API accepts it as-is, stores its converted value in out.
func convertField(field string, raw json.RawMessage, out map[string]any) error {
	switch field {
	case "messages":
	case "model", "user": // Model is in the URL; user is client metadata.
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return fmt.Errorf("cloudflare: %s must be a string", field)
		}
	case "stream":
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			return fmt.Errorf("cloudflare: stream must be a boolean")
		}
	case "max_tokens", "max_completion_tokens", "top_k", "seed":
		return convertIntegerField(field, raw, out)
	case "temperature", "top_p", "frequency_penalty", "presence_penalty", "repetition_penalty":
		return convertBoundedFloatField(field, raw, out)
	case "lora":
		var value string
		if json.Unmarshal(raw, &value) != nil || value == "" {
			return fmt.Errorf("cloudflare: lora must be a nonempty string")
		}
		out[field] = value
	case "n":
		var value int
		if json.Unmarshal(raw, &value) != nil || value != 1 {
			return unsupported(field)
		}
	case "logprobs":
		var value bool
		if json.Unmarshal(raw, &value) != nil || value {
			return unsupported(field)
		}
	case "stream_options":
		var options map[string]json.RawMessage
		if json.Unmarshal(raw, &options) != nil || options == nil {
			return fmt.Errorf("cloudflare: stream_options must be an object")
		}
		for name, option := range options {
			var value bool
			if name != "include_usage" || json.Unmarshal(option, &value) != nil || string(option) == "null" {
				return unsupported("stream_options." + name)
			}
		}
	default:
		return unsupported(field)
	}
	return nil
}

func convertIntegerField(field string, raw json.RawMessage, out map[string]any) error {
	var value int64
	if json.Unmarshal(raw, &value) != nil || value < 0 || (field != "seed" && value == 0) {
		bound := "positive"
		if field == "seed" {
			bound = "nonnegative"
		}
		return fmt.Errorf("cloudflare: %s must be %s", field, bound)
	}
	if field != "max_completion_tokens" {
		out[field] = value
	}
	return nil
}

func convertBoundedFloatField(field string, raw json.RawMessage, out map[string]any) error {
	var value float64
	if json.Unmarshal(raw, &value) != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("cloudflare: %s must be a finite number", field)
	}
	if (field == "temperature" && (value < 0 || value > 5)) || (field == "top_p" && (value < 0 || value > 1)) ||
		(field == "repetition_penalty" && value <= 0) {
		return fmt.Errorf("cloudflare: %s is out of range", field)
	}
	out[field] = value
	return nil
}

// convertMessages validates the OpenAI-style messages array and converts it
// to Cloudflare's flat role/content shape.
func convertMessages(raw json.RawMessage) ([]map[string]string, error) {
	var messages []map[string]json.RawMessage
	if json.Unmarshal(raw, &messages) != nil || len(messages) == 0 {
		return nil, fmt.Errorf("cloudflare: at least one message is required")
	}
	converted := make([]map[string]string, 0, len(messages))
	for _, msg := range messages {
		for field, value := range msg {
			if field != "role" && field != "content" && string(value) != "null" {
				return nil, unsupported("messages." + field)
			}
		}
		var role string
		if json.Unmarshal(msg["role"], &role) != nil {
			return nil, fmt.Errorf("cloudflare: message role is required")
		}
		if role == "developer" {
			role = "system"
		}
		if role != "system" && role != "user" && role != "assistant" {
			return nil, fmt.Errorf("cloudflare: unsupported message role %q", role)
		}
		content, err := textContent(msg["content"])
		if err != nil {
			return nil, err
		}
		converted = append(converted, map[string]string{"role": role, "content": content})
	}
	return converted, nil
}

func textContent(raw json.RawMessage) (string, error) {
	var text string
	if len(raw) == 0 || string(raw) == "null" {
		return "", fmt.Errorf("cloudflare: message content must be text")
	}
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var parts []map[string]json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return "", fmt.Errorf("cloudflare: message content must be text or text parts")
	}
	for _, part := range parts {
		for field, value := range part {
			if field != "type" && field != "text" && string(value) != "null" {
				return "", unsupported("messages.content." + field)
			}
		}
		var kind, value string
		if json.Unmarshal(part["type"], &kind) != nil || kind != "text" || json.Unmarshal(part["text"], &value) != nil {
			return "", fmt.Errorf("cloudflare: only text message content is supported")
		}
		text += value
	}
	return text, nil
}

func clientWantsUsage(data []byte) bool {
	var in struct {
		StreamOptions struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}
	return json.Unmarshal(data, &in) == nil && in.StreamOptions.IncludeUsage
}
