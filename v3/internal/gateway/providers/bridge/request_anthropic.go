package bridge

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

func anthropicRequest(root gjson.Result, c *chatRequest) error {
	if err := allowedFields(root, "model messages system stream max_tokens temperature top_p stop_sequences tools tool_choice thinking"); err != nil {
		return err
	}
	c.MaxTokens = intOption(root.Get("max_tokens"))
	c.Temperature = floatOption(root.Get("temperature"))
	c.TopP = floatOption(root.Get("top_p"))
	c.Stop = raw(root.Get("stop_sequences"))
	if thinking := root.Get("thinking"); thinking.Exists() && thinking.Get("type").Str != "disabled" {
		return fmt.Errorf("bridge: Anthropic thinking budget requires native upstream")
	}
	if err := anthropicSystem(root, c); err != nil {
		return err
	}
	if err := anthropicTools(root, c); err != nil {
		return err
	}
	if err := anthropicToolChoice(root, c); err != nil {
		return err
	}
	return anthropicMessages(root, c)
}

func anthropicSystem(root gjson.Result, c *chatRequest) error {
	system := root.Get("system")
	if !system.Exists() {
		return nil
	}
	m := chatMessage{Role: "system"}
	if system.Type == gjson.String {
		m.Content = textContent(system.Str)
	} else {
		var parts []contentPart
		for _, block := range system.Array() {
			if block.Get("type").Str != "text" || block.Get("cache_control").Exists() {
				return fmt.Errorf("bridge: Anthropic system block cannot be converted losslessly")
			}
			if err := allowedFields(block, "type text"); err != nil {
				return err
			}
			appendText(&parts, block.Get("text").Str)
		}
		appendParts(&m, parts)
	}
	c.Messages = append(c.Messages, m)
	return nil
}

func anthropicTools(root gjson.Result, c *chatRequest) error {
	for _, tool := range root.Get("tools").Array() {
		if err := allowedFields(tool, "name description input_schema"); err != nil {
			return err
		}
		c.Tools = append(c.Tools, chatTool{Type: "function", Function: functionSpec{Name: tool.Get("name").Str, Description: tool.Get("description").Str, Parameters: raw(tool.Get("input_schema"))}})
	}
	return nil
}

func anthropicToolChoice(root gjson.Result, c *chatRequest) error {
	choice := root.Get("tool_choice")
	if !choice.Exists() {
		return nil
	}
	if err := allowedFields(choice, "type name disable_parallel_tool_use"); err != nil {
		return err
	}
	switch choice.Get("type").Str {
	case "auto":
		c.ToolChoice = textContent("auto")
	case "any":
		c.ToolChoice = textContent("required")
	case "none":
		c.ToolChoice = textContent("none")
	case "tool":
		c.ToolChoice = namedToolChoice(choice.Get("name").Str)
	default:
		return fmt.Errorf("bridge: unsupported Anthropic tool choice")
	}
	if choice.Get("disable_parallel_tool_use").Exists() {
		v := !choice.Get("disable_parallel_tool_use").Bool()
		c.ParallelToolCalls = &v
	}
	return nil
}

func anthropicMessages(root gjson.Result, c *chatRequest) error {
	for _, item := range root.Get("messages").Array() {
		if err := anthropicReqMessage(item, c); err != nil {
			return err
		}
	}
	return nil
}

func anthropicReqMessage(item gjson.Result, c *chatRequest) error {
	if err := allowedFields(item, "role content"); err != nil {
		return err
	}
	m := chatMessage{Role: item.Get("role").Str}
	content := item.Get("content")
	if content.Type == gjson.String {
		m.Content = textContent(content.Str)
		c.Messages = append(c.Messages, m)
		return nil
	}
	var parts []contentPart
	flush := func() {
		appendParts(&m, parts)
		if len(parts) > 0 || len(m.ToolCalls) > 0 {
			c.Messages = append(c.Messages, m)
		}
		m = chatMessage{Role: item.Get("role").Str}
		parts = nil
	}
	for _, block := range content.Array() {
		if block.Get("cache_control").Exists() {
			return fmt.Errorf("bridge: Anthropic cache_control requires native upstream")
		}
		if err := anthropicContentBlock(block, &m, &parts, flush, c); err != nil {
			return err
		}
	}
	flush()
	return nil
}

func anthropicContentBlock(block gjson.Result, m *chatMessage, parts *[]contentPart, flush func(), c *chatRequest) error {
	switch block.Get("type").Str {
	case "text":
		if err := allowedFields(block, "type text"); err != nil {
			return err
		}
		appendText(parts, block.Get("text").Str)
		return nil
	case "image":
		return anthropicImageBlock(block, parts)
	case "tool_use":
		return anthropicToolUseBlock(block, m)
	case "tool_result":
		return anthropicToolResultBlock(block, flush, c)
	default:
		return fmt.Errorf("bridge: Anthropic content %q requires native upstream", block.Get("type").Str)
	}
}

func anthropicImageBlock(block gjson.Result, parts *[]contentPart) error {
	if err := allowedFields(block, "type source"); err != nil {
		return err
	}
	source := block.Get("source")
	if err := allowedFields(source, "type url media_type data"); err != nil {
		return err
	}
	url := source.Get("url").Str
	if source.Get("type").Str == "base64" {
		mime := source.Get("media_type").Str
		if !strings.HasPrefix(mime, "image/") {
			return fmt.Errorf("bridge: invalid Anthropic image media type")
		}
		url = "data:" + mime + ";base64," + source.Get("data").Str
	} else if source.Get("type").Str != "url" {
		return fmt.Errorf("bridge: unsupported Anthropic image source")
	}
	*parts = append(*parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: url}})
	return nil
}

func anthropicToolUseBlock(block gjson.Result, m *chatMessage) error {
	if err := allowedFields(block, "type id name input"); err != nil {
		return err
	}
	if !block.Get("input").IsObject() {
		return fmt.Errorf("bridge: Anthropic tool input must be an object")
	}
	m.ToolCalls = append(m.ToolCalls, chatToolCall{ID: block.Get("id").Str, Type: "function", Function: functionCall{Name: block.Get("name").Str, Arguments: block.Get("input").Raw}})
	return nil
}

func anthropicToolResultBlock(block gjson.Result, flush func(), c *chatRequest) error {
	if err := allowedFields(block, "type tool_use_id content is_error"); err != nil {
		return err
	}
	if block.Get("is_error").Bool() {
		return fmt.Errorf("bridge: Anthropic error tool results require native upstream")
	}
	flush()
	result := block.Get("content")
	var text string
	if result.Type == gjson.String {
		text = result.Str
	} else {
		for _, part := range result.Array() {
			if err := allowedFields(part, "type text"); err != nil {
				return err
			}
			if part.Get("type").Str != "text" {
				return fmt.Errorf("bridge: non-text Anthropic tool result requires native upstream")
			}
			text += part.Get("text").Str
		}
	}
	c.Messages = append(c.Messages, chatMessage{Role: "tool", ToolCallID: block.Get("tool_use_id").Str, Content: textContent(text)})
	return nil
}
