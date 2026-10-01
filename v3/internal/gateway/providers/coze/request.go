package coze

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func convertRequest(req *gateway.Request, bot string) ([]byte, string, error) {
	root := gjson.ParseBytes(req.Body)
	if !gjson.ValidBytes(req.Body) || !root.IsObject() || len(req.Body) > maxResponseSize {
		return nil, "", fmt.Errorf("coze: requires a valid Chat JSON object within the size limit")
	}
	if err := validateTopLevelRequestFields(root); err != nil {
		return nil, "", err
	}
	converted, err := convertRequestMessages(root)
	if err != nil {
		return nil, "", err
	}
	user, err := requestUser(req, root)
	if err != nil {
		return nil, "", err
	}
	conversation := root.Get("conversation_id")
	if conversation.Exists() && conversation.Type != gjson.Null && conversation.Type != gjson.String {
		return nil, "", fmt.Errorf("coze: conversation_id must be a string")
	}
	out := map[string]any{"bot_id": bot, "user_id": user, "additional_messages": converted, "stream": true}
	for _, name := range []string{"custom_variables", "meta_data", "extra_params", "shortcut_command", "parameters"} {
		if v := root.Get(name); v.Exists() && v.Type != gjson.Null {
			if !v.IsObject() {
				return nil, "", fmt.Errorf("coze: %s must be an object", name)
			}
			out[name] = json.RawMessage(v.Raw)
		}
	}
	if v := root.Get("auto_save_history"); v.Exists() && v.Type != gjson.Null {
		if v.Type != gjson.True && v.Type != gjson.False {
			return nil, "", fmt.Errorf("coze: auto_save_history must be boolean")
		}
		out["auto_save_history"] = v.Bool()
	}
	body, err := json.Marshal(out)
	return body, conversation.Str, err
}

// validateTopLevelRequestFields rejects any Chat field Coze cannot honor,
// since generation and tool configuration live on the bot instead.
func validateTopLevelRequestFields(root gjson.Result) error {
	allowed := map[string]bool{"model": true, "messages": true, "stream": true, "stream_options": true, "user": true,
		"conversation_id": true, "custom_variables": true, "auto_save_history": true, "meta_data": true,
		"extra_params": true, "shortcut_command": true, "parameters": true, "n": true}
	var fieldErr error
	root.ForEach(func(key, value gjson.Result) bool {
		if !allowed[key.Str] && value.Type != gjson.Null {
			fieldErr = fmt.Errorf("coze: does not support request field %q; configure generation and tools on the bot", key.Str)
			return false
		}
		return true
	})
	if fieldErr != nil {
		return fieldErr
	}
	if n := root.Get("n"); n.Exists() && n.Type != gjson.Null && (n.Type != gjson.Number || n.Float() != 1) {
		return fmt.Errorf("coze: supports one completion only")
	}
	options := root.Get("stream_options")
	if !options.Exists() || options.Type == gjson.Null {
		return nil
	}
	if !options.IsObject() {
		return fmt.Errorf("coze: stream_options must be an object")
	}
	options.ForEach(func(key, value gjson.Result) bool {
		if key.Str != "include_usage" || (value.Type != gjson.True && value.Type != gjson.False) {
			fieldErr = fmt.Errorf("coze: supports boolean stream_options.include_usage only")
		}
		return fieldErr == nil
	})
	return fieldErr
}

// convertRequestMessages validates and converts the Chat messages array into
// Coze's additional_messages shape.
func convertRequestMessages(root gjson.Result) ([]map[string]string, error) {
	messages := root.Get("messages")
	if !messages.IsArray() || len(messages.Array()) == 0 {
		return nil, fmt.Errorf("coze: requires at least one message")
	}
	converted := make([]map[string]string, 0, len(messages.Array()))
	for _, message := range messages.Array() {
		if !message.IsObject() {
			return nil, fmt.Errorf("coze: messages must be objects")
		}
		role := message.Get("role").Str
		if role != "user" && role != "assistant" {
			return nil, fmt.Errorf("coze: supports user and assistant history only; configure instructions on the bot")
		}
		var fieldErr error
		message.ForEach(func(key, value gjson.Result) bool {
			if key.Str != "role" && key.Str != "content" && value.Type != gjson.Null {
				fieldErr = fmt.Errorf("coze: does not support message field %q", key.Str)
			}
			return fieldErr == nil
		})
		if fieldErr != nil {
			return nil, fieldErr
		}
		text, err := textContent(message.Get("content"))
		if err != nil {
			return nil, err
		}
		m := map[string]string{"role": role, "content": text, "content_type": "text"}
		if role == "assistant" {
			m["type"] = "answer"
		}
		converted = append(converted, m)
	}
	return converted, nil
}

func requestUser(req *gateway.Request, root gjson.Result) (string, error) {
	user := req.ID
	if user == "" {
		user = "codego"
	}
	supplied := root.Get("user")
	if !supplied.Exists() || supplied.Type == gjson.Null {
		return user, nil
	}
	if supplied.Type != gjson.String {
		return "", fmt.Errorf("coze: user must be a string")
	}
	if supplied.Str != "" {
		user = supplied.Str
	}
	return user, nil
}

func textContent(content gjson.Result) (string, error) {
	if content.Type == gjson.String {
		return content.Str, nil
	}
	if !content.IsArray() {
		return "", fmt.Errorf("coze: messages require text content")
	}
	var out strings.Builder
	for _, part := range content.Array() {
		if !part.IsObject() || part.Get("type").Str != "text" || part.Get("text").Type != gjson.String {
			return "", fmt.Errorf("coze: supports text content only")
		}
		valid := true
		part.ForEach(func(key, _ gjson.Result) bool { valid = key.Str == "type" || key.Str == "text"; return valid })
		if !valid {
			return "", fmt.Errorf("coze: text content has unsupported fields")
		}
		out.WriteString(part.Get("text").Str)
	}
	return out.String(), nil
}
