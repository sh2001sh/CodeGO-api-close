package bridge

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

func geminiRequest(root gjson.Result, c *chatRequest) error {
	if err := allowedFields(root, "contents systemInstruction system_instruction generationConfig generation_config tools toolConfig tool_config"); err != nil {
		return err
	}
	if err := geminiGenerationConfig(root, c); err != nil {
		return err
	}
	if err := geminiSystemInstruction(root, c); err != nil {
		return err
	}
	if err := geminiFunctionDeclarations(root, c); err != nil {
		return err
	}
	if err := geminiToolConfig(root, c); err != nil {
		return err
	}
	return geminiContents(root, c)
}

func geminiGenerationConfig(root gjson.Result, c *chatRequest) error {
	config := root.Get("generationConfig")
	if !config.Exists() {
		config = root.Get("generation_config")
	}
	if !config.Exists() {
		return nil
	}
	if err := allowedFields(config, "maxOutputTokens temperature topP stopSequences responseMimeType"); err != nil {
		return err
	}
	c.MaxTokens = intOption(config.Get("maxOutputTokens"))
	c.Temperature = floatOption(config.Get("temperature"))
	c.TopP = floatOption(config.Get("topP"))
	c.Stop = raw(config.Get("stopSequences"))
	if mime := config.Get("responseMimeType").Str; mime == "application/json" {
		c.ResponseFormat = []byte(`{"type":"json_object"}`)
	} else if mime != "" && mime != "text/plain" {
		return fmt.Errorf("bridge: Gemini response MIME type requires native upstream")
	}
	return nil
}

func geminiSystemInstruction(root gjson.Result, c *chatRequest) error {
	system := root.Get("systemInstruction")
	if !system.Exists() {
		system = root.Get("system_instruction")
	}
	if !system.Exists() {
		return nil
	}
	var parts []contentPart
	for _, part := range system.Get("parts").Array() {
		if err := allowedFields(part, "text"); err != nil {
			return err
		}
		appendText(&parts, part.Get("text").Str)
	}
	m := chatMessage{Role: "system"}
	appendParts(&m, parts)
	c.Messages = append(c.Messages, m)
	return nil
}

func geminiFunctionDeclarations(root gjson.Result, c *chatRequest) error {
	for _, tool := range root.Get("tools").Array() {
		if err := allowedFields(tool, "functionDeclarations"); err != nil {
			return err
		}
		for _, f := range tool.Get("functionDeclarations").Array() {
			if err := allowedFields(f, "name description parameters"); err != nil {
				return err
			}
			parameters, err := geminiSchema(f.Get("parameters"))
			if err != nil {
				return err
			}
			c.Tools = append(c.Tools, chatTool{Type: "function", Function: functionSpec{Name: f.Get("name").Str, Description: f.Get("description").Str, Parameters: parameters}})
		}
	}
	return nil
}

func geminiToolConfig(root gjson.Result, c *chatRequest) error {
	toolConfig := root.Get("toolConfig")
	if !toolConfig.Exists() {
		toolConfig = root.Get("tool_config")
	}
	if !toolConfig.Exists() {
		return nil
	}
	if err := allowedFields(toolConfig, "functionCallingConfig"); err != nil {
		return err
	}
	fc := toolConfig.Get("functionCallingConfig")
	if err := allowedFields(fc, "mode allowedFunctionNames"); err != nil {
		return err
	}
	names := fc.Get("allowedFunctionNames").Array()
	if len(names) > 1 {
		return fmt.Errorf("bridge: Gemini allowedFunctionNames list requires native upstream")
	}
	if len(names) == 1 {
		c.ToolChoice = namedToolChoice(names[0].Str)
		return nil
	}
	switch fc.Get("mode").Str {
	case "AUTO", "":
		c.ToolChoice = textContent("auto")
	case "ANY":
		c.ToolChoice = textContent("required")
	case "NONE":
		c.ToolChoice = textContent("none")
	default:
		return fmt.Errorf("bridge: unsupported Gemini function calling mode")
	}
	return nil
}

// geminiCallState tracks pending function-call IDs (keyed by function name)
// and a fallback sequence counter, shared across all contents entries so a
// functionResponse can be matched to the functionCall that preceded it.
type geminiCallState struct {
	ids map[string][]string
	seq int
}

func geminiContents(root gjson.Result, c *chatRequest) error {
	state := &geminiCallState{ids: make(map[string][]string)}
	for _, content := range root.Get("contents").Array() {
		if err := geminiReqContent(content, c, state); err != nil {
			return err
		}
	}
	return nil
}

func geminiReqContent(content gjson.Result, c *chatRequest, state *geminiCallState) error {
	if err := allowedFields(content, "role parts"); err != nil {
		return err
	}
	role := content.Get("role").Str
	if role == "model" {
		role = "assistant"
	}
	if role == "" {
		role = "user"
	}
	m := chatMessage{Role: role}
	var parts []contentPart
	flush := func() {
		appendParts(&m, parts)
		if len(parts) > 0 || len(m.ToolCalls) > 0 {
			c.Messages = append(c.Messages, m)
		}
		m = chatMessage{Role: role}
		parts = nil
	}
	for _, part := range content.Get("parts").Array() {
		if err := geminiReqPart(part, &m, &parts, flush, c, state); err != nil {
			return err
		}
	}
	flush()
	return nil
}

func geminiReqPart(part gjson.Result, m *chatMessage, parts *[]contentPart, flush func(), c *chatRequest, state *geminiCallState) error {
	if err := allowedFields(part, "text inlineData fileData functionCall functionResponse"); err != nil {
		return err
	}
	switch {
	case part.Get("text").Exists():
		appendText(parts, part.Get("text").Str)
		return nil
	case part.Get("inlineData").Exists():
		return geminiInlineData(part.Get("inlineData"), parts)
	case part.Get("fileData").Exists():
		return geminiFileData(part.Get("fileData"), parts)
	case part.Get("functionCall").Exists():
		return geminiReqFunctionCall(part.Get("functionCall"), m, state)
	case part.Get("functionResponse").Exists():
		flush()
		return geminiFunctionResponse(part.Get("functionResponse"), c, state)
	default:
		return fmt.Errorf("bridge: unsupported Gemini part")
	}
}

func geminiInlineData(source gjson.Result, parts *[]contentPart) error {
	if err := allowedFields(source, "mimeType data"); err != nil {
		return err
	}
	mime := source.Get("mimeType").Str
	if !strings.HasPrefix(mime, "image/") {
		return fmt.Errorf("bridge: Gemini non-image inline data requires native upstream")
	}
	*parts = append(*parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: "data:" + mime + ";base64," + source.Get("data").Str}})
	return nil
}

func geminiFileData(source gjson.Result, parts *[]contentPart) error {
	if err := allowedFields(source, "mimeType fileUri"); err != nil {
		return err
	}
	if !strings.HasPrefix(source.Get("mimeType").Str, "image/") {
		return fmt.Errorf("bridge: Gemini non-image file data requires native upstream")
	}
	uri := source.Get("fileUri").Str
	if !strings.HasPrefix(uri, "https://") && !strings.HasPrefix(uri, "http://") {
		return fmt.Errorf("bridge: Gemini private file URI requires native upstream")
	}
	*parts = append(*parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: source.Get("fileUri").Str}})
	return nil
}

func geminiReqFunctionCall(f gjson.Result, m *chatMessage, state *geminiCallState) error {
	if err := allowedFields(f, "name args id"); err != nil {
		return err
	}
	if f.Get("args").Exists() && !f.Get("args").IsObject() {
		return fmt.Errorf("bridge: Gemini function arguments must be an object")
	}
	name := f.Get("name").Str
	id := f.Get("id").Str
	if id == "" {
		id = fmt.Sprintf("call_%d", state.seq)
	}
	state.seq++
	state.ids[name] = append(state.ids[name], id)
	arguments := f.Get("args").Raw
	if arguments == "" {
		arguments = "{}"
	}
	m.ToolCalls = append(m.ToolCalls, chatToolCall{ID: id, Type: "function", Function: functionCall{Name: name, Arguments: arguments}})
	return nil
}

func geminiFunctionResponse(f gjson.Result, c *chatRequest, state *geminiCallState) error {
	if err := allowedFields(f, "name response id"); err != nil {
		return err
	}
	ids := state.ids[f.Get("name").Str]
	if len(ids) == 0 {
		return fmt.Errorf("bridge: Gemini function response has no preceding call")
	}
	index := 0
	if id := f.Get("id").Str; id != "" {
		index = -1
		for n, prior := range ids {
			if prior == id {
				index = n
				break
			}
		}
		if index < 0 {
			return fmt.Errorf("bridge: Gemini function response ID has no preceding call")
		}
	}
	id := ids[index]
	state.ids[f.Get("name").Str] = append(ids[:index], ids[index+1:]...)
	c.Messages = append(c.Messages, chatMessage{Role: "tool", ToolCallID: id, Content: textContent(f.Get("response").Raw)})
	return nil
}
