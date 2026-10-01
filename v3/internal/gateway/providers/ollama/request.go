package ollama

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

func convertRequest(data []byte, model string, stream bool) ([]byte, error) {
	if err := validateFields(data); err != nil {
		return nil, err
	}
	var in chatRequest
	if err := json.Unmarshal(data, &in); err != nil {
		return nil, fmt.Errorf("ollama: invalid Chat request: %w", err)
	}
	if len(in.Messages) == 0 {
		return nil, fmt.Errorf("ollama: at least one message is required")
	}
	out := nativeRequest{Model: model, Stream: stream, Options: map[string]any{}, Think: in.Think, KeepAlive: in.KeepAlive}
	if err := applySamplingOptions(in, &out); err != nil {
		return nil, err
	}
	if err := applyResponseFormat(in, &out); err != nil {
		return nil, err
	}
	if err := convertChatMessages(in, &out); err != nil {
		return nil, err
	}
	if err := convertTools(in, &out); err != nil {
		return nil, err
	}
	if err := applyToolChoice(in, &out); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// applySamplingOptions copies the Chat sampling parameters that map onto
// Ollama's generic options map.
func applySamplingOptions(in chatRequest, out *nativeRequest) error {
	if in.Temperature != nil {
		out.Options["temperature"] = *in.Temperature
	}
	if in.TopP != nil {
		out.Options["top_p"] = *in.TopP
	}
	if in.TopK != nil {
		out.Options["top_k"] = *in.TopK
	}
	if in.FrequencyPenalty != nil {
		out.Options["frequency_penalty"] = *in.FrequencyPenalty
	}
	if in.PresencePenalty != nil {
		out.Options["presence_penalty"] = *in.PresencePenalty
	}
	if in.Seed != nil {
		out.Options["seed"] = *in.Seed
	}
	maxTokens := in.MaxTokens
	if in.MaxCompletionTokens != nil {
		maxTokens = in.MaxCompletionTokens
	}
	if maxTokens != nil {
		if *maxTokens <= 0 {
			return fmt.Errorf("ollama: max_tokens must be positive")
		}
		out.Options["num_predict"] = *maxTokens
	}
	if len(in.Stop) > 0 && string(in.Stop) != "null" {
		var stops []string
		var one string
		if json.Unmarshal(in.Stop, &one) == nil {
			stops = []string{one}
		} else if err := json.Unmarshal(in.Stop, &stops); err != nil {
			return fmt.Errorf("ollama: stop must be a string or string array")
		}
		out.Options["stop"] = stops
	}
	return nil
}

func applyResponseFormat(in chatRequest, out *nativeRequest) error {
	if in.ResponseFormat == nil {
		return nil
	}
	switch in.ResponseFormat.Type {
	case "text":
	case "json", "json_object":
		out.Format = "json"
	case "json_schema":
		if in.ResponseFormat.JSONSchema == nil || !gjson.ParseBytes(in.ResponseFormat.JSONSchema.Schema).IsObject() {
			return fmt.Errorf("ollama: json_schema requires an object schema")
		}
		out.Format = in.ResponseFormat.JSONSchema.Schema
	default:
		return fmt.Errorf("ollama: unsupported response_format")
	}
	return nil
}

func convertChatMessages(in chatRequest, out *nativeRequest) error {
	toolNames := map[string]string{}
	for _, msg := range in.Messages {
		for _, call := range msg.ToolCalls {
			if call.ID != "" {
				toolNames[call.ID] = call.Function.Name
			}
		}
	}
	for _, msg := range in.Messages {
		m, err := convertChatMessage(msg, toolNames)
		if err != nil {
			return err
		}
		out.Messages = append(out.Messages, m)
	}
	return nil
}

func convertChatMessage(msg chatMessage, toolNames map[string]string) (nativeMessage, error) {
	content, images, err := convertContent(msg.Content)
	if err != nil {
		return nativeMessage{}, err
	}
	m := nativeMessage{Role: msg.Role, Content: content, Images: images}
	switch msg.Role {
	case "developer":
		m.Role = "system"
	case "system", "user", "assistant":
	case "tool":
		m.ToolName = msg.Name
		if m.ToolName == "" {
			m.ToolName = toolNames[msg.ToolCallID]
		}
		if m.ToolName == "" {
			return nativeMessage{}, fmt.Errorf("ollama: tool result requires a name or matching tool_call_id")
		}
	default:
		return nativeMessage{}, fmt.Errorf("ollama: unsupported message role %q", msg.Role)
	}
	if len(msg.ToolCalls) > 0 && msg.Role != "assistant" {
		return nativeMessage{}, fmt.Errorf("ollama: tool_calls are allowed only in assistant messages")
	}
	for _, call := range msg.ToolCalls {
		if call.Type != "function" || call.Function.Name == "" || !gjson.Valid(call.Function.Arguments) || !gjson.Parse(call.Function.Arguments).IsObject() {
			return nativeMessage{}, fmt.Errorf("ollama: tool calls require a named function and JSON object arguments")
		}
		m.ToolCalls = append(m.ToolCalls, nativeToolCall{Function: nativeToolFunction{
			Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments),
		}})
	}
	return m, nil
}

func convertTools(in chatRequest, out *nativeRequest) error {
	for _, t := range in.Tools {
		if t.Type != "function" || t.Function.Name == "" {
			return fmt.Errorf("ollama: only named function tools are supported")
		}
		if t.Function.Strict != nil && *t.Function.Strict {
			return fmt.Errorf("ollama: strict tool schemas are unsupported")
		}
		if len(t.Function.Parameters) != 0 && !gjson.ParseBytes(t.Function.Parameters).IsObject() {
			return fmt.Errorf("ollama: tool parameters must be a JSON object")
		}
		t.Function.Strict = nil
		out.Tools = append(out.Tools, t)
	}
	return nil
}

func applyToolChoice(in chatRequest, out *nativeRequest) error {
	if len(in.ToolChoice) == 0 || string(in.ToolChoice) == "null" {
		return nil
	}
	var choice string
	if json.Unmarshal(in.ToolChoice, &choice) != nil {
		return fmt.Errorf("ollama: named tool_choice is unsupported")
	}
	switch choice {
	case "auto":
	case "none":
		out.Tools = nil
	default:
		return fmt.Errorf("ollama: unsupported tool_choice %q", choice)
	}
	return nil
}

func convertContent(raw json.RawMessage) (string, []string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil, nil
	}
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain, nil, nil
	}
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", nil, fmt.Errorf("ollama: message content must be text or an array")
	}
	var content strings.Builder
	var images []string
	for _, part := range parts {
		switch part.Type {
		case "text":
			content.WriteString(part.Text)
		case "image_url":
			if part.ImageURL == nil {
				return "", nil, fmt.Errorf("ollama: image_url requires an inline data URL")
			}
			url := part.ImageURL.URL
			prefix, encoded, ok := strings.Cut(url, ",")
			if !ok || !strings.HasPrefix(prefix, "data:image/") || !strings.HasSuffix(prefix, ";base64") {
				return "", nil, fmt.Errorf("ollama: images require base64 data URLs")
			}
			if len(encoded) == 0 {
				return "", nil, fmt.Errorf("ollama: image data is empty")
			}
			if _, err := base64.StdEncoding.DecodeString(encoded); err != nil {
				return "", nil, fmt.Errorf("ollama: image data is not valid base64")
			}
			images = append(images, encoded)
		default:
			return "", nil, fmt.Errorf("ollama: unsupported content type %q", part.Type)
		}
	}
	return content.String(), images, nil
}

func validateFields(data []byte) error {
	root := gjson.ParseBytes(data)
	if !gjson.ValidBytes(data) || !root.IsObject() {
		return fmt.Errorf("ollama: invalid Chat JSON")
	}
	var invalid string
	root.ForEach(func(k, v gjson.Result) bool {
		if v.Type == gjson.Null {
			return true
		}
		switch k.Str {
		case "model", "messages", "stream", "max_tokens", "max_completion_tokens", "temperature", "top_p", "top_k", "frequency_penalty", "presence_penalty", "seed", "stop", "tools", "tool_choice", "response_format", "think", "keep_alive", "user":
		case "stream_options":
			v.ForEach(func(option, _ gjson.Result) bool {
				if option.Str != "include_usage" {
					invalid = "stream_options." + option.Str
				}
				return invalid == ""
			})
		case "n":
			if v.Int() != 1 {
				invalid = k.Str
			}
		case "logprobs":
			if v.Bool() {
				invalid = k.Str
			}
		case "parallel_tool_calls":
			if !v.Bool() {
				invalid = k.Str
			}
		default:
			invalid = k.Str
		}
		return invalid == ""
	})
	for _, msg := range root.Get("messages").Array() {
		msg.ForEach(func(k, v gjson.Result) bool {
			if v.Type != gjson.Null {
				switch k.Str {
				case "role", "content", "name", "tool_call_id", "tool_calls":
				default:
					invalid = "messages." + k.Str
				}
			}
			return invalid == ""
		})
	}
	if invalid != "" {
		return fmt.Errorf("ollama: unsupported Chat field %q", invalid)
	}
	return nil
}

func clientWantsUsage(data []byte) bool {
	return gjson.GetBytes(data, "stream_options.include_usage").Bool()
}
