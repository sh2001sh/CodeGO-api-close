package gemini

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

func chatRequest(body []byte) ([]byte, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("gemini: invalid chat JSON")
	}
	root := gjson.ParseBytes(body)
	if err := validateUnsupportedChatFields(root); err != nil {
		return nil, err
	}
	messages := root.Get("messages")
	if !messages.IsArray() || len(messages.Array()) == 0 {
		return nil, fmt.Errorf("gemini: messages must be a nonempty array")
	}
	out := generateRequest{}
	callNames := make(map[string]string)
	for _, message := range messages.Array() {
		if err := appendChatMessage(message, &out, callNames); err != nil {
			return nil, err
		}
	}
	if len(out.Contents) == 0 {
		return nil, fmt.Errorf("gemini: request has no conversation content")
	}
	var err error
	out.Config, err = chatConfig(root)
	if err != nil {
		return nil, err
	}
	if err := chatTools(root, &out); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// validateUnsupportedChatFields rejects Chat request fields that Gemini
// chat conversion cannot honor.
func validateUnsupportedChatFields(root gjson.Result) error {
	for _, key := range []string{"logprobs", "top_logprobs", "logit_bias", "audio", "prediction", "functions", "function_call"} {
		if root.Get(key).Exists() && root.Get(key).Type != gjson.Null {
			return fmt.Errorf("gemini: unsupported chat field %s", key)
		}
	}
	if modalities := root.Get("modalities"); modalities.Exists() && modalities.Raw != `["text"]` {
		return fmt.Errorf("gemini: only text output is supported by chat conversion")
	}
	if root.Get("parallel_tool_calls").Exists() && !root.Get("parallel_tool_calls").Bool() {
		return fmt.Errorf("gemini: disabling parallel tool calls is unsupported")
	}
	return nil
}

// appendChatMessage converts one Chat message into Gemini contents (or folds
// it into out.System for system/developer roles), tracking tool-call names
// by ID in callNames so a later "tool" message can be matched to its call.
func appendChatMessage(message gjson.Result, out *generateRequest, callNames map[string]string) error {
	role := message.Get("role").Str
	if role == "tool" {
		name := callNames[message.Get("tool_call_id").Str]
		if name == "" {
			return fmt.Errorf("gemini: tool result has no matching function call")
		}
		result, err := toolResult(message.Get("content"))
		if err != nil {
			return err
		}
		out.Contents = append(out.Contents, content{Role: "user", Parts: []part{{FunctionResponse: &functionResponse{Name: name, Response: result}}}})
		return nil
	}
	parts, err := messageParts(message.Get("content"))
	if err != nil {
		return err
	}
	if role == "system" || role == "developer" {
		for _, p := range parts {
			if p.InlineData != nil || p.FileData != nil {
				return fmt.Errorf("gemini: system instructions support only text")
			}
		}
		if out.System == nil {
			out.System = &content{Parts: []part{}}
		}
		out.System.Parts = append(out.System.Parts, parts...)
		return nil
	}
	switch role {
	case "assistant":
		role = "model"
	case "user":
	default:
		return fmt.Errorf("gemini: unsupported message role %q", role)
	}
	for _, call := range message.Get("tool_calls").Array() {
		if role != "model" || call.Get("type").Str != "function" {
			return fmt.Errorf("gemini: only assistant function calls are supported")
		}
		name, id := call.Get("function.name").Str, call.Get("id").Str
		args := call.Get("function.arguments").Str
		if name == "" || id == "" || !gjson.Valid(args) || !gjson.Parse(args).IsObject() {
			return fmt.Errorf("gemini: function call requires name, id and JSON object arguments")
		}
		callNames[id] = name
		parts = append(parts, part{FunctionCall: &functionCall{Name: name, Args: json.RawMessage(args)},
			ThoughtSignature: call.Get("extra_content.google.thought_signature").Str})
	}
	if len(parts) == 0 {
		return fmt.Errorf("gemini: message has no supported content")
	}
	if signature := message.Get("extra_content.google.thought_signature").Str; signature != "" {
		parts[0].ThoughtSignature = signature
	}
	out.Contents = append(out.Contents, content{Role: role, Parts: parts})
	return nil
}

func messageParts(value gjson.Result) ([]part, error) {
	if value.Type == gjson.Null || !value.Exists() {
		return nil, nil
	}
	if value.Type == gjson.String {
		if value.Str == "" {
			return nil, nil
		}
		return []part{{Text: value.Str}}, nil
	}
	if !value.IsArray() {
		return nil, fmt.Errorf("gemini: message content must be text or an array")
	}
	var parts []part
	for _, item := range value.Array() {
		switch item.Get("type").Str {
		case "text":
			text := item.Get("text")
			if text.Type != gjson.String {
				return nil, fmt.Errorf("gemini: text content must be a string")
			}
			if text.Str != "" {
				parts = append(parts, part{Text: text.Str})
			}
		case "image_url":
			if detail := item.Get("image_url.detail").Str; detail != "" && detail != "auto" {
				return nil, fmt.Errorf("gemini: image detail setting is unsupported")
			}
			uri := item.Get("image_url.url").Str
			if !strings.HasPrefix(uri, "data:") {
				return nil, fmt.Errorf("gemini: chat images require an inline base64 data URL")
			}
			header, data, ok := strings.Cut(strings.TrimPrefix(uri, "data:"), ",")
			if !ok || !strings.HasSuffix(header, ";base64") || !strings.HasPrefix(header, "image/") {
				return nil, fmt.Errorf("gemini: invalid image data URL")
			}
			if _, err := base64.StdEncoding.DecodeString(data); err != nil {
				return nil, fmt.Errorf("gemini: invalid image base64")
			}
			parts = append(parts, part{InlineData: &inlineData{MIMEType: strings.TrimSuffix(header, ";base64"), Data: data}})
		default:
			return nil, fmt.Errorf("gemini: unsupported message content type %q", item.Get("type").Str)
		}
	}
	return parts, nil
}

func toolResult(value gjson.Result) (json.RawMessage, error) {
	if value.Type != gjson.String {
		return nil, fmt.Errorf("gemini: tool result content must be text")
	}
	if gjson.Valid(value.Str) && gjson.Parse(value.Str).IsObject() {
		return json.RawMessage(value.Str), nil
	}
	return json.Marshal(struct {
		Output string `json:"output"`
	}{value.Str})
}
