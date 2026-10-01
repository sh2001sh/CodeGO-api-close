package cohere

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

type inputMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCalls  []toolCall      `json:"tool_calls"`
	ToolCallID string          `json:"tool_call_id"`
}

type chatRequest struct {
	Messages            []inputMessage  `json:"messages"`
	MaxTokens           *int64          `json:"max_tokens"`
	MaxCompletionTokens *int64          `json:"max_completion_tokens"`
	Temperature         *float64        `json:"temperature"`
	TopP                *float64        `json:"top_p"`
	TopK                *int64          `json:"top_k"`
	FrequencyPenalty    *float64        `json:"frequency_penalty"`
	PresencePenalty     *float64        `json:"presence_penalty"`
	Seed                *int64          `json:"seed"`
	Stop                json.RawMessage `json:"stop"`
	Tools               []functionTool  `json:"tools"`
	ToolChoice          json.RawMessage `json:"tool_choice"`
	ResponseFormat      json.RawMessage `json:"response_format"`
}

type nativeMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type functionTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
		Strict      bool            `json:"strict,omitempty"`
	} `json:"function"`
}

type nativeRequest struct {
	Model            string          `json:"model"`
	Messages         []nativeMessage `json:"messages"`
	Stream           bool            `json:"stream"`
	MaxTokens        *int64          `json:"max_tokens,omitempty"`
	Temperature      *float64        `json:"temperature,omitempty"`
	P                *float64        `json:"p,omitempty"`
	K                *int64          `json:"k,omitempty"`
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`
	Seed             *int64          `json:"seed,omitempty"`
	StopSequences    []string        `json:"stop_sequences,omitempty"`
	Tools            []functionTool  `json:"tools,omitempty"`
	ToolChoice       string          `json:"tool_choice,omitempty"`
	ResponseFormat   json.RawMessage `json:"response_format,omitempty"`
}

func convertRequest(body []byte, model string, stream bool) ([]byte, error) {
	if err := validateFields(body); err != nil {
		return nil, err
	}
	var in chatRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, fmt.Errorf("cohere: invalid Chat request: %w", err)
	}
	if len(in.Messages) == 0 {
		return nil, errors.New("cohere: messages are required")
	}
	out := nativeRequest{Model: model, Stream: stream, Temperature: in.Temperature, P: in.TopP, K: in.TopK,
		FrequencyPenalty: in.FrequencyPenalty, PresencePenalty: in.PresencePenalty, Seed: in.Seed, MaxTokens: in.MaxTokens}
	if in.MaxCompletionTokens != nil {
		out.MaxTokens = in.MaxCompletionTokens
	}
	if out.MaxTokens != nil && *out.MaxTokens <= 0 {
		return nil, errors.New("cohere: max_tokens must be positive")
	}
	if err := appendMessages(&out, in.Messages); err != nil {
		return nil, err
	}
	if err := appendTools(&out, in.Tools); err != nil {
		return nil, err
	}
	if err := applyToolChoice(&out, in.ToolChoice); err != nil {
		return nil, err
	}
	if err := applyStop(&out, in.Stop); err != nil {
		return nil, err
	}
	if err := applyResponseFormat(&out, in.ResponseFormat); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// appendMessages converts and validates the OpenAI-style messages, tracking
// tool call IDs so later tool-result messages can be matched against them.
func appendMessages(out *nativeRequest, messages []inputMessage) error {
	knownCalls := make(map[string]bool)
	for _, msg := range messages {
		content, err := textContent(msg.Content)
		if err != nil {
			return err
		}
		m := nativeMessage{Role: msg.Role, Content: content, ToolCalls: msg.ToolCalls, ToolCallID: msg.ToolCallID}
		if m.Role == "developer" {
			m.Role = "system"
		}
		switch m.Role {
		case "system", "user", "assistant":
		case "tool":
			if !knownCalls[m.ToolCallID] {
				return errors.New("cohere: tool result needs a preceding matching tool call")
			}
		default:
			return fmt.Errorf("cohere: unsupported message role %q", msg.Role)
		}
		if len(m.ToolCalls) > 0 && m.Role != "assistant" {
			return errors.New("cohere: only assistant messages may contain tool calls")
		}
		for _, call := range m.ToolCalls {
			if call.ID == "" || call.Type != "function" || call.Function.Name == "" || !gjson.Valid(call.Function.Arguments) || !gjson.Parse(call.Function.Arguments).IsObject() {
				return errors.New("cohere: tool calls require an id, named function and object arguments")
			}
			knownCalls[call.ID] = true
		}
		out.Messages = append(out.Messages, m)
	}
	return nil
}

func appendTools(out *nativeRequest, tools []functionTool) error {
	for _, tool := range tools {
		if tool.Type != "function" || tool.Function.Name == "" || tool.Function.Strict || !gjson.ParseBytes(tool.Function.Parameters).IsObject() {
			return errors.New("cohere: tools require a named function and object schema; strict tool schemas are unsupported")
		}
		out.Tools = append(out.Tools, tool)
	}
	return nil
}

func applyToolChoice(out *nativeRequest, raw json.RawMessage) error {
	if !meaningful(raw) {
		return nil
	}
	var choice string
	if json.Unmarshal(raw, &choice) != nil {
		return errors.New("cohere: named tool choice is unsupported")
	}
	switch choice {
	case "auto":
	case "required":
		out.ToolChoice = "REQUIRED"
	case "none":
		out.ToolChoice = "NONE"
	default:
		return errors.New("cohere: unsupported tool choice")
	}
	return nil
}

func applyStop(out *nativeRequest, raw json.RawMessage) error {
	if !meaningful(raw) {
		return nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		out.StopSequences = []string{one}
		return nil
	}
	if json.Unmarshal(raw, &out.StopSequences) != nil {
		return errors.New("cohere: stop must be a string or string array")
	}
	return nil
}

func applyResponseFormat(out *nativeRequest, raw json.RawMessage) error {
	if !meaningful(raw) {
		return nil
	}
	r := gjson.ParseBytes(raw)
	switch r.Get("type").Str {
	case "text":
	case "json_object":
		out.ResponseFormat = json.RawMessage(`{"type":"json_object"}`)
	case "json_schema":
		if r.Get("json_schema.strict").Bool() || !r.Get("json_schema.schema").IsObject() {
			return errors.New("cohere: response schema requires an object and cannot guarantee strict mode")
		}
		encoded, err := json.Marshal(struct {
			Type   string          `json:"type"`
			Schema json.RawMessage `json:"schema"`
		}{"json_object", json.RawMessage(r.Get("json_schema.schema").Raw)})
		if err != nil {
			return err
		}
		out.ResponseFormat = encoded
	default:
		return errors.New("cohere: unsupported response format")
	}
	return nil
}

func meaningful(raw json.RawMessage) bool {
	v := strings.TrimSpace(string(raw))
	return v != "" && v != "null" && v != "[]"
}

func textContent(raw json.RawMessage) (string, error) {
	if !meaningful(raw) {
		return "", nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return "", errors.New("cohere: message content must be text")
	}
	var result strings.Builder
	for _, part := range parts {
		if part.Type != "text" {
			return "", errors.New("cohere: non-text message content is unsupported")
		}
		result.WriteString(part.Text)
	}
	return result.String(), nil
}

func validateFields(body []byte) error {
	root := gjson.ParseBytes(body)
	if !gjson.ValidBytes(body) || !root.IsObject() {
		return errors.New("cohere: body must be a JSON object")
	}
	var unsupported string
	root.ForEach(func(key, value gjson.Result) bool {
		if value.Type == gjson.Null {
			return true
		}
		switch key.Str {
		case "model", "messages", "stream", "max_tokens", "max_completion_tokens", "temperature", "top_p", "top_k", "frequency_penalty", "presence_penalty", "seed", "stop", "tools", "tool_choice", "response_format", "user":
		case "stream_options":
			value.ForEach(func(k, _ gjson.Result) bool {
				if k.Str != "include_usage" {
					unsupported = "stream_options." + k.Str
				}
				return unsupported == ""
			})
		case "n":
			if value.Int() != 1 {
				unsupported = key.Str
			}
		case "logprobs":
			if value.Bool() {
				unsupported = key.Str
			}
		case "parallel_tool_calls":
			if !value.Bool() {
				unsupported = key.Str
			}
		default:
			unsupported = key.Str
		}
		return unsupported == ""
	})
	for _, message := range root.Get("messages").Array() {
		message.ForEach(func(key, value gjson.Result) bool {
			if value.Type != gjson.Null {
				switch key.Str {
				case "role", "content", "tool_calls", "tool_call_id":
				default:
					unsupported = "messages." + key.Str
				}
			}
			return unsupported == ""
		})
	}
	if unsupported != "" {
		return fmt.Errorf("cohere: unsupported Chat field %q", unsupported)
	}
	return nil
}
