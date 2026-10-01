package bridge

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
)

func responsesRequest(root gjson.Result, c *chatRequest) error {
	if err := allowedFields(root, "model input instructions stream max_output_tokens temperature top_p tools tool_choice parallel_tool_calls reasoning text"); err != nil {
		return err
	}
	c.MaxTokens = intOption(root.Get("max_output_tokens"))
	c.Temperature = floatOption(root.Get("temperature"))
	c.TopP = floatOption(root.Get("top_p"))
	c.ParallelToolCalls = boolOption(root.Get("parallel_tool_calls"))
	if v := root.Get("instructions"); v.Exists() && v.Type != gjson.Null {
		c.Messages = append(c.Messages, chatMessage{Role: "system", Content: textContent(v.Str)})
	}
	if v := root.Get("reasoning"); v.Exists() {
		if err := allowedFields(v, "effort"); err != nil {
			return err
		}
		c.ReasoningEffort = v.Get("effort").Str
	}
	if err := responsesTextFormat(root, c); err != nil {
		return err
	}
	if err := responsesTools(root, c); err != nil {
		return err
	}
	if err := responsesToolChoice(root, c); err != nil {
		return err
	}
	return responsesInput(root, c)
}

func responsesTextFormat(root gjson.Result, c *chatRequest) error {
	v := root.Get("text")
	if !v.Exists() {
		return nil
	}
	if err := allowedFields(v, "format"); err != nil {
		return err
	}
	format := v.Get("format")
	switch format.Get("type").Str {
	case "", "text":
	case "json_object":
		c.ResponseFormat = json.RawMessage(`{"type":"json_object"}`)
	case "json_schema":
		if err := allowedFields(format, "type name description schema strict"); err != nil {
			return err
		}
		schema, _ := json.Marshal(struct {
			Name        string          `json:"name"`
			Description string          `json:"description,omitempty"`
			Schema      json.RawMessage `json:"schema"`
			Strict      *bool           `json:"strict,omitempty"`
		}{Name: format.Get("name").Str, Description: format.Get("description").Str, Schema: raw(format.Get("schema")), Strict: boolOption(format.Get("strict"))})
		c.ResponseFormat, _ = json.Marshal(struct {
			Type   string          `json:"type"`
			Schema json.RawMessage `json:"json_schema"`
		}{"json_schema", schema})
	default:
		return fmt.Errorf("bridge: unsupported Responses text format")
	}
	return nil
}

func responsesTools(root gjson.Result, c *chatRequest) error {
	for _, tool := range root.Get("tools").Array() {
		if tool.Get("type").Str != "function" {
			return fmt.Errorf("bridge: Responses tool %q requires a native upstream", tool.Get("type").Str)
		}
		if err := allowedFields(tool, "type name description parameters strict"); err != nil {
			return err
		}
		c.Tools = append(c.Tools, chatTool{Type: "function", Function: functionSpec{Name: tool.Get("name").Str, Description: tool.Get("description").Str, Parameters: raw(tool.Get("parameters")), Strict: boolOption(tool.Get("strict"))}})
	}
	return nil
}

func responsesToolChoice(root gjson.Result, c *chatRequest) error {
	choice := root.Get("tool_choice")
	if choice.Type == gjson.String {
		c.ToolChoice = raw(choice)
	} else if choice.Exists() {
		if choice.Get("type").Str != "function" {
			return fmt.Errorf("bridge: unsupported Responses tool choice")
		}
		c.ToolChoice = namedToolChoice(choice.Get("name").Str)
	}
	return nil
}

func responsesInput(root gjson.Result, c *chatRequest) error {
	input := root.Get("input")
	if input.Type == gjson.String {
		c.Messages = append(c.Messages, chatMessage{Role: "user", Content: textContent(input.Str)})
		return nil
	}
	if !input.IsArray() {
		return fmt.Errorf("bridge: Responses input must be a string or array")
	}
	for _, item := range input.Array() {
		if err := responsesInputItem(item, c); err != nil {
			return err
		}
	}
	return nil
}

func responsesInputItem(item gjson.Result, c *chatRequest) error {
	switch item.Get("type").Str {
	case "", "message":
		return responsesInputMessage(item, c)
	case "function_call":
		if err := allowedFields(item, "type call_id name arguments id status"); err != nil {
			return err
		}
		c.Messages = append(c.Messages, chatMessage{Role: "assistant", ToolCalls: []chatToolCall{{ID: item.Get("call_id").Str, Type: "function", Function: functionCall{Name: item.Get("name").Str, Arguments: item.Get("arguments").Str}}}})
		return nil
	case "function_call_output":
		if err := allowedFields(item, "type call_id output id status"); err != nil {
			return err
		}
		output := item.Get("output")
		if output.Type != gjson.String {
			return fmt.Errorf("bridge: structured Responses tool output requires native upstream")
		}
		c.Messages = append(c.Messages, chatMessage{Role: "tool", ToolCallID: item.Get("call_id").Str, Content: textContent(output.Str)})
		return nil
	default:
		return fmt.Errorf("bridge: Responses input item %q requires native upstream", item.Get("type").Str)
	}
}

func responsesInputMessage(item gjson.Result, c *chatRequest) error {
	if err := allowedFields(item, "type role content"); err != nil {
		return err
	}
	m := chatMessage{Role: item.Get("role").Str}
	if m.Role == "" {
		m.Role = "user"
	}
	content := item.Get("content")
	if content.Type == gjson.String {
		m.Content = textContent(content.Str)
	} else {
		var parts []contentPart
		for _, block := range content.Array() {
			if err := responsesContentBlock(block, &parts); err != nil {
				return err
			}
		}
		appendParts(&m, parts)
	}
	c.Messages = append(c.Messages, m)
	return nil
}

func responsesContentBlock(block gjson.Result, parts *[]contentPart) error {
	switch block.Get("type").Str {
	case "input_text", "output_text":
		if err := allowedFields(block, "type text"); err != nil {
			return err
		}
		appendText(parts, block.Get("text").Str)
		return nil
	case "input_image":
		if err := allowedFields(block, "type image_url detail file_id"); err != nil {
			return err
		}
		if block.Get("file_id").Exists() {
			return fmt.Errorf("bridge: Responses image file_id requires native upstream")
		}
		*parts = append(*parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: block.Get("image_url").Str, Detail: block.Get("detail").Str}})
		return nil
	default:
		return fmt.Errorf("bridge: unsupported Responses content type %q", block.Get("type").Str)
	}
}

func namedToolChoice(name string) json.RawMessage {
	b, _ := json.Marshal(struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}{Type: "function", Function: struct {
		Name string `json:"name"`
	}{name}})
	return b
}
