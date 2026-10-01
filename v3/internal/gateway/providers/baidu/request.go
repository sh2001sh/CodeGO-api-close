package baidu

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Messages        []message       `json:"messages"`
	Temperature     *float64        `json:"temperature,omitempty"`
	TopP            *float64        `json:"top_p,omitempty"`
	PenaltyScore    *float64        `json:"penalty_score,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	System          string          `json:"system,omitempty"`
	MaxOutputTokens *int64          `json:"max_output_tokens,omitempty"`
	User            json.RawMessage `json:"user_id,omitempty"`
}

func convertRequest(body []byte, stream bool) ([]byte, error) {
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, invalid("invalid_json", "baidu: invalid request JSON")
	}
	root := gjson.ParseBytes(body)
	if err := validateTopLevelFields(root); err != nil {
		return nil, err
	}
	if err := validateStreamOptions(root); err != nil {
		return nil, err
	}
	out := chatRequest{Stream: stream}
	if err := populateMessages(root, &out); err != nil {
		return nil, err
	}
	if err := applyNumericFields(root, &out); err != nil {
		return nil, err
	}
	if err := applyMaxTokens(root, &out); err != nil {
		return nil, err
	}
	if user := root.Get("user"); user.Exists() && user.Type != gjson.Null {
		out.User = json.RawMessage(user.Raw)
	}
	return json.Marshal(out)
}

// validateTopLevelFields rejects any request field baidu cannot represent,
// including the no-op fields that must still carry their default values.
func validateTopLevelFields(root gjson.Result) error {
	var fieldErr error
	root.ForEach(func(key, value gjson.Result) bool {
		if value.Type == gjson.Null {
			return true
		}
		switch key.Str {
		case "model", "messages", "temperature", "top_p", "frequency_penalty", "max_tokens", "max_completion_tokens", "user", "stream", "stream_options", "n", "tools", "functions", "tool_choice", "function_call", "response_format", "stop":
		case "presence_penalty":
			if value.Type != gjson.Number || value.Float() != 0 {
				fieldErr = invalid("unsupported_request", "baidu: presence_penalty is unsupported")
			}
		case "logprobs":
			if value.Type != gjson.False {
				fieldErr = invalid("unsupported_request", "baidu: logprobs are unsupported")
			}
		default:
			fieldErr = invalid("unsupported_request", "baidu: unsupported "+key.Str)
		}
		return fieldErr == nil
	})
	if fieldErr != nil {
		return fieldErr
	}
	for _, field := range []string{"tools", "functions", "tool_choice", "function_call", "response_format", "stop"} {
		value := root.Get(field)
		if value.Exists() && value.Type != gjson.Null && (!value.IsArray() || len(value.Array()) != 0) {
			return invalid("unsupported_request", "baidu: unsupported "+field)
		}
	}
	if n := root.Get("n"); n.Exists() && (n.Type != gjson.Number || n.Float() != 1) {
		return invalid("unsupported_request", "baidu: n must be 1")
	}
	return nil
}

func validateStreamOptions(root gjson.Result) error {
	options := root.Get("stream_options")
	if !options.Exists() || options.Type == gjson.Null {
		return nil
	}
	if !options.IsObject() {
		return invalid("invalid_request", "baidu: stream_options must be an object")
	}
	var fieldErr error
	options.ForEach(func(key, value gjson.Result) bool {
		if key.Str != "include_usage" || (value.Type != gjson.True && value.Type != gjson.False) {
			fieldErr = invalid("unsupported_request", "baidu: unsupported stream option")
		}
		return fieldErr == nil
	})
	return fieldErr
}

// populateMessages validates and converts the OpenAI-style messages array,
// folding system messages into out.System and the rest into out.Messages.
func populateMessages(root gjson.Result, out *chatRequest) error {
	messages := root.Get("messages")
	if !messages.IsArray() || len(messages.Array()) == 0 {
		return invalid("invalid_messages", "baidu: messages are required")
	}
	for _, m := range messages.Array() {
		role := m.Get("role").Str
		if role != "system" && role != "user" && role != "assistant" {
			return invalid("unsupported_request", "baidu: unsupported message role")
		}
		if len(m.Get("tool_calls").Array()) > 0 || m.Get("function_call").Exists() {
			return invalid("unsupported_request", "baidu: tool messages are unsupported")
		}
		content, err := textContent(m.Get("content"))
		if err != nil {
			return err
		}
		if role == "system" {
			if out.System != "" {
				out.System += "\n"
			}
			out.System += content
		} else {
			out.Messages = append(out.Messages, message{Role: role, Content: content})
		}
	}
	if len(out.Messages) == 0 {
		return invalid("invalid_messages", "baidu: at least one non-system message is required")
	}
	return nil
}

func applyNumericFields(root gjson.Result, out *chatRequest) error {
	for field, target := range map[string]**float64{"temperature": &out.Temperature, "top_p": &out.TopP, "frequency_penalty": &out.PenaltyScore} {
		value := root.Get(field)
		if !value.Exists() || value.Type == gjson.Null {
			continue
		}
		if value.Type != gjson.Number {
			return invalid("invalid_request", "baidu: "+field+" must be numeric")
		}
		n := value.Float()
		*target = &n
	}
	return nil
}

func applyMaxTokens(root gjson.Result, out *chatRequest) error {
	maxTokens := root.Get("max_completion_tokens")
	if !maxTokens.Exists() {
		maxTokens = root.Get("max_tokens")
	}
	if !maxTokens.Exists() || maxTokens.Type == gjson.Null {
		return nil
	}
	n := maxTokens.Int()
	if maxTokens.Type != gjson.Number || n < 1 || maxTokens.Float() != float64(n) {
		return invalid("invalid_request", "baidu: max tokens must be a positive integer")
	}
	if n == 1 {
		n = 2 // legacy Wenxin minimum
	}
	out.MaxOutputTokens = &n
	return nil
}

func textContent(content gjson.Result) (string, error) {
	if content.Type == gjson.String {
		return content.Str, nil
	}
	if !content.IsArray() {
		return "", invalid("unsupported_request", "baidu: message content must be text")
	}
	var text strings.Builder
	for _, part := range content.Array() {
		if part.Get("type").Str != "text" || part.Get("text").Type != gjson.String {
			return "", invalid("unsupported_request", "baidu: non-text content is unsupported")
		}
		text.WriteString(part.Get("text").Str)
	}
	return text.String(), nil
}
