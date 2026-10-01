package responses

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func chatRequestError(message string) error {
	return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "unsupported_request", Message: "responses: " + message}
}

func chatFields(root gjson.Result, fields string) error {
	if !root.IsObject() {
		return chatRequestError("expected a JSON object")
	}
	allowed := " " + fields + " "
	var err error
	root.ForEach(func(key, _ gjson.Result) bool {
		if !strings.Contains(allowed, " "+key.Str+" ") {
			err = chatRequestError(fmt.Sprintf("field %q cannot be converted losslessly", key.Str))
		}
		return err == nil
	})
	return err
}

func chatRequestBody(req *gateway.Request) ([]byte, error) {
	if !gjson.ValidBytes(req.Body) {
		return nil, chatRequestError("invalid JSON")
	}
	root := gjson.ParseBytes(req.Body)
	if err := chatFields(root, "model messages stream stream_options max_tokens max_completion_tokens temperature top_p tools tool_choice parallel_tool_calls reasoning_effort response_format n user metadata store service_tier"); err != nil {
		return nil, err
	}
	if n := root.Get("n"); n.Exists() && n.Int() != 1 {
		return nil, chatRequestError("n must be 1")
	}
	if options := root.Get("stream_options"); options.Exists() {
		if err := chatFields(options, "include_usage"); err != nil {
			return nil, err
		}
	}
	input, err := chatMessages(root.Get("messages"))
	if err != nil {
		return nil, err
	}
	out := map[string]any{"model": req.Model, "input": input, "stream": req.Stream}
	for _, field := range strings.Fields("temperature top_p parallel_tool_calls user metadata store service_tier") {
		if value := root.Get(field); value.Exists() {
			out[field] = json.RawMessage(value.Raw)
		}
	}
	if err := applyMaxTokens(root, out); err != nil {
		return nil, err
	}
	if effort := root.Get("reasoning_effort"); effort.Exists() {
		out["reasoning"] = map[string]any{"effort": json.RawMessage(effort.Raw)}
	}
	if err := applyToolFields(root, out); err != nil {
		return nil, err
	}
	if format := root.Get("response_format"); format.Exists() {
		converted, err := chatFormat(format)
		if err != nil {
			return nil, err
		}
		out["text"] = map[string]any{"format": converted}
	}
	return json.Marshal(out)
}

func applyMaxTokens(root gjson.Result, out map[string]any) error {
	maxTokens := root.Get("max_completion_tokens")
	if !maxTokens.Exists() {
		maxTokens = root.Get("max_tokens")
	} else if old := root.Get("max_tokens"); old.Exists() && old.Int() != maxTokens.Int() {
		return chatRequestError("max_tokens and max_completion_tokens conflict")
	}
	if maxTokens.Exists() {
		out["max_output_tokens"] = json.RawMessage(maxTokens.Raw)
	}
	return nil
}

func applyToolFields(root gjson.Result, out map[string]any) error {
	if tools := root.Get("tools"); tools.Exists() {
		converted, err := chatTools(tools)
		if err != nil {
			return err
		}
		out["tools"] = converted
	}
	choice := root.Get("tool_choice")
	if !choice.Exists() {
		return nil
	}
	if choice.Type == gjson.String {
		out["tool_choice"] = choice.Str
		return nil
	}
	if err := chatFields(choice, "type function"); err != nil {
		return err
	}
	fn := choice.Get("function")
	if err := chatFields(fn, "name"); err != nil {
		return err
	}
	if choice.Get("type").Str != "function" || fn.Get("name").Str == "" {
		return chatRequestError("tool_choice requires a function name")
	}
	out["tool_choice"] = map[string]any{"type": "function", "name": fn.Get("name").Str}
	return nil
}

func chatTools(tools gjson.Result) ([]any, error) {
	if !tools.IsArray() {
		return nil, chatRequestError("tools must be an array")
	}
	out := make([]any, 0, len(tools.Array()))
	for _, tool := range tools.Array() {
		if err := chatFields(tool, "type function"); err != nil {
			return nil, err
		}
		fn := tool.Get("function")
		if err := chatFields(fn, "name description parameters strict"); err != nil {
			return nil, err
		}
		if tool.Get("type").Str != "function" || fn.Get("name").Str == "" {
			return nil, chatRequestError("only named function tools are supported")
		}
		converted := map[string]any{"type": "function", "strict": false}
		fn.ForEach(func(key, value gjson.Result) bool {
			converted[key.Str] = json.RawMessage(value.Raw)
			return true
		})
		out = append(out, converted)
	}
	return out, nil
}

func chatFormat(format gjson.Result) (any, error) {
	if err := chatFields(format, "type json_schema"); err != nil {
		return nil, err
	}
	switch typ := format.Get("type").Str; typ {
	case "text", "json_object":
		if format.Get("json_schema").Exists() {
			return nil, chatRequestError("json_schema requires a json_schema format")
		}
		return map[string]any{"type": typ}, nil
	case "json_schema":
		schema := format.Get("json_schema")
		if err := chatFields(schema, "name description schema strict"); err != nil {
			return nil, err
		}
		if schema.Get("name").Str == "" || !schema.Get("schema").IsObject() {
			return nil, chatRequestError("json_schema requires name and schema")
		}
		converted := map[string]any{"type": typ}
		schema.ForEach(func(key, value gjson.Result) bool {
			converted[key.Str] = json.RawMessage(value.Raw)
			return true
		})
		return converted, nil
	default:
		return nil, chatRequestError("unsupported response_format")
	}
}
