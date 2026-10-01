package zhipu

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tidwall/gjson"
)

type nativeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type nativeRequest struct {
	Prompt      []nativeMessage `json:"prompt"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	RequestID   string          `json:"request_id,omitempty"`
	Incremental bool            `json:"incremental"`
}

func convertRequest(body []byte, stream bool) ([]byte, error) {
	root := gjson.ParseBytes(body)
	if !gjson.ValidBytes(body) || !root.IsObject() {
		return nil, errors.New("zhipu: body must be a JSON object")
	}
	if err := validateFields(root); err != nil {
		return nil, err
	}
	input, err := samplingParams(body)
	if err != nil {
		return nil, err
	}
	out := nativeRequest{Temperature: input.Temperature, TopP: input.TopP, RequestID: input.RequestID, Incremental: stream}
	if err := appendPrompt(&out, root.Get("messages")); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// validateFields rejects any top-level request field zhipu cannot represent.
func validateFields(root gjson.Result) error {
	var fieldErr error
	root.ForEach(func(key, value gjson.Result) bool {
		if value.Type == gjson.Null {
			return true
		}
		switch key.Str {
		case "model", "messages", "stream", "temperature", "top_p", "request_id":
		case "stream_options":
			if !value.IsObject() {
				fieldErr = errors.New("zhipu: stream_options must be an object")
				break
			}
			value.ForEach(func(k, v gjson.Result) bool {
				if k.Str != "include_usage" || (v.Type != gjson.True && v.Type != gjson.False) {
					fieldErr = fmt.Errorf("zhipu: unsupported stream option %q", k.Str)
				}
				return fieldErr == nil
			})
		case "n":
			if value.Raw != "1" {
				fieldErr = errors.New("zhipu: only one choice is supported")
			}
		case "logprobs":
			if value.Type != gjson.False {
				fieldErr = errors.New("zhipu: logprobs are unsupported")
			}
		default:
			fieldErr = fmt.Errorf("zhipu: unsupported Chat field %q", key.Str)
		}
		return fieldErr == nil
	})
	return fieldErr
}

func samplingParams(body []byte) (struct {
	Temperature *float64 `json:"temperature"`
	TopP        *float64 `json:"top_p"`
	RequestID   string   `json:"request_id"`
}, error) {
	var input struct {
		Temperature *float64 `json:"temperature"`
		TopP        *float64 `json:"top_p"`
		RequestID   string   `json:"request_id"`
	}
	if err := json.Unmarshal(body, &input); err != nil {
		return input, errors.New("zhipu: invalid sampling parameters")
	}
	if input.TopP != nil && (*input.TopP < 0 || *input.TopP > 1) {
		return input, errors.New("zhipu: top_p must be between zero and one")
	}
	if input.TopP != nil && *input.TopP == 1 {
		*input.TopP = 0.99 // native v3 rejects the inclusive OpenAI upper bound
	}
	if input.Temperature != nil && (*input.Temperature < 0 || *input.Temperature > 1) {
		return input, errors.New("zhipu: temperature must be between zero and one")
	}
	return input, nil
}

// appendPrompt converts the OpenAI-style messages array into out.Prompt,
// inserting the legacy adapter's required filler user turn after a system
// message.
func appendPrompt(out *nativeRequest, messages gjson.Result) error {
	if !messages.IsArray() || len(messages.Array()) == 0 {
		return errors.New("zhipu: messages are required")
	}
	for _, msg := range messages.Array() {
		m, err := convertMessage(msg)
		if err != nil {
			return err
		}
		out.Prompt = append(out.Prompt, m)
		if m.Role == "system" {
			// Preserve the legacy adapter's required user turn after a system prompt.
			out.Prompt = append(out.Prompt, nativeMessage{Role: "user", Content: "Okay"})
		}
	}
	return nil
}

func convertMessage(msg gjson.Result) (nativeMessage, error) {
	if !msg.IsObject() {
		return nativeMessage{}, errors.New("zhipu: messages must be objects")
	}
	var err error
	msg.ForEach(func(k, v gjson.Result) bool {
		if k.Str != "role" && k.Str != "content" && v.Type != gjson.Null {
			err = fmt.Errorf("zhipu: unsupported message field %q", k.Str)
		}
		return err == nil
	})
	if err != nil {
		return nativeMessage{}, err
	}
	role := msg.Get("role").Str
	if role == "developer" {
		role = "system"
	}
	if role != "user" && role != "assistant" && role != "system" {
		return nativeMessage{}, errors.New("zhipu: unsupported message role")
	}
	content := msg.Get("content")
	if content.Type == gjson.String {
		return nativeMessage{Role: role, Content: content.Str}, nil
	}
	if !content.IsArray() {
		return nativeMessage{}, errors.New("zhipu: message content must be text")
	}
	text := ""
	for _, part := range content.Array() {
		if !part.IsObject() || part.Get("type").Str != "text" || part.Get("text").Type != gjson.String {
			return nativeMessage{}, errors.New("zhipu: non-text content is unsupported")
		}
		part.ForEach(func(k, _ gjson.Result) bool {
			if k.Str != "type" && k.Str != "text" {
				err = fmt.Errorf("zhipu: unsupported content field %q", k.Str)
			}
			return err == nil
		})
		if err != nil {
			return nativeMessage{}, err
		}
		text += part.Get("text").Str
	}
	return nativeMessage{Role: role, Content: text}, nil
}
