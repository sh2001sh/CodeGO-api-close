package responses

import (
	"encoding/json"

	"github.com/tidwall/gjson"
)

func chatMessages(messages gjson.Result) ([]any, error) {
	if !messages.IsArray() || len(messages.Array()) == 0 {
		return nil, chatRequestError("messages must be a nonempty array")
	}
	input := make([]any, 0, len(messages.Array()))
	for _, message := range messages.Array() {
		if err := chatFields(message, "role content tool_calls tool_call_id refusal"); err != nil {
			return nil, err
		}
		role := message.Get("role").Str
		if role == "tool" {
			if message.Get("tool_calls").Exists() || message.Get("refusal").Exists() {
				return nil, chatRequestError("tool messages cannot contain tool_calls or refusal")
			}
			if message.Get("tool_call_id").Str == "" || message.Get("content").Type != gjson.String {
				return nil, chatRequestError("tool messages require tool_call_id and string content")
			}
			input = append(input, map[string]any{"type": "function_call_output", "call_id": message.Get("tool_call_id").Str, "output": message.Get("content").Str})
			continue
		}
		if role != "user" && role != "assistant" && role != "system" && role != "developer" {
			return nil, chatRequestError("unsupported message role")
		}
		if message.Get("tool_call_id").Exists() || (role != "assistant" && (message.Get("tool_calls").Exists() || message.Get("refusal").Exists())) {
			return nil, chatRequestError("tool calls and refusal require an assistant message")
		}
		parts, err := chatContent(message.Get("content"), role)
		if err != nil {
			return nil, err
		}
		if refusal := message.Get("refusal"); refusal.Exists() && refusal.Type != gjson.Null {
			if refusal.Type != gjson.String {
				return nil, chatRequestError("refusal must be a string")
			}
			parts = append(parts, map[string]any{"type": "refusal", "refusal": refusal.Str})
		}
		if len(parts) > 0 {
			input = append(input, map[string]any{"type": "message", "role": role, "content": parts})
		}
		calls := message.Get("tool_calls")
		if calls.Exists() {
			if !calls.IsArray() {
				return nil, chatRequestError("tool_calls must be an array")
			}
			for _, call := range calls.Array() {
				converted, err := chatFunctionCall(call)
				if err != nil {
					return nil, err
				}
				input = append(input, converted)
			}
		}
		if len(parts) == 0 && len(calls.Array()) == 0 {
			return nil, chatRequestError("message requires content or tool_calls")
		}
	}
	return input, nil
}

func chatContent(content gjson.Result, role string) ([]any, error) {
	typ := "input_text"
	if role == "assistant" {
		typ = "output_text"
	}
	if !content.Exists() || content.Type == gjson.Null {
		return nil, nil
	}
	if content.Type == gjson.String {
		return []any{map[string]any{"type": typ, "text": content.Str}}, nil
	}
	if !content.IsArray() {
		return nil, chatRequestError("message content must be a string or array")
	}
	parts := make([]any, 0, len(content.Array()))
	for _, part := range content.Array() {
		converted, err := chatContentPart(part, role, typ)
		if err != nil {
			return nil, err
		}
		parts = append(parts, converted)
	}
	return parts, nil
}

func chatContentPart(part gjson.Result, role, textType string) (any, error) {
	switch part.Get("type").Str {
	case "text":
		if err := chatFields(part, "type text"); err != nil {
			return nil, err
		}
		if part.Get("text").Type != gjson.String {
			return nil, chatRequestError("text content must be a string")
		}
		return map[string]any{"type": textType, "text": part.Get("text").Str}, nil
	case "image_url":
		if role != "user" {
			return nil, chatRequestError("image content requires a user message")
		}
		if err := chatFields(part, "type image_url"); err != nil {
			return nil, err
		}
		image := part.Get("image_url")
		if err := chatFields(image, "url detail"); err != nil {
			return nil, err
		}
		if image.Get("url").Str == "" {
			return nil, chatRequestError("image_url requires a URL")
		}
		converted := map[string]any{"type": "input_image", "image_url": image.Get("url").Str}
		if detail := image.Get("detail"); detail.Exists() {
			converted["detail"] = json.RawMessage(detail.Raw)
		}
		return converted, nil
	case "refusal":
		if role != "assistant" {
			return nil, chatRequestError("refusal content requires an assistant message")
		}
		if err := chatFields(part, "type refusal"); err != nil {
			return nil, err
		}
		if part.Get("refusal").Type != gjson.String {
			return nil, chatRequestError("refusal content must be a string")
		}
		return map[string]any{"type": "refusal", "refusal": part.Get("refusal").Str}, nil
	default:
		return nil, chatRequestError("unsupported message content type")
	}
}

func chatFunctionCall(call gjson.Result) (any, error) {
	if err := chatFields(call, "id type function"); err != nil {
		return nil, err
	}
	fn := call.Get("function")
	if err := chatFields(fn, "name arguments"); err != nil {
		return nil, err
	}
	if call.Get("type").Str != "function" || call.Get("id").Str == "" || fn.Get("name").Str == "" || fn.Get("arguments").Type != gjson.String {
		return nil, chatRequestError("tool call requires id, function name and string arguments")
	}
	return map[string]any{"type": "function_call", "call_id": call.Get("id").Str, "name": fn.Get("name").Str, "arguments": fn.Get("arguments").Str}, nil
}
