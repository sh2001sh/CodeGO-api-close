package gateway

import (
	"strings"

	"github.com/tidwall/gjson"
)

// Read only prompt-bearing protocol fields. Tool schemas, metadata, image/file
// URLs and encoded media are not text prompts and must not cause false matches.
func sensitivePromptText(req *Request) string {
	if text, handled := sensitiveAuxiliaryPrompt(req); handled {
		return text
	}
	root := gjson.ParseBytes(req.Body)
	texts := []string{}
	appendText := func(value gjson.Result) {
		if value.Type == gjson.String && value.String() != "" {
			texts = append(texts, value.String())
		}
	}
	appendContent := func(content gjson.Result) {
		if content.Type == gjson.String {
			appendText(content)
			return
		}
		for _, part := range content.Array() {
			switch part.Get("type").String() {
			case "text", "input_text", "output_text":
				appendText(part.Get("text"))
			}
		}
	}
	switch req.Protocol {
	case ProtocolOpenAIChat:
		for _, message := range root.Get("messages").Array() {
			appendContent(message.Get("content"))
		}
	case ProtocolResponses:
		input := root.Get("input")
		if input.Type == gjson.String {
			appendText(input)
		} else {
			for _, item := range input.Array() {
				switch item.Get("type").String() {
				case "", "message":
					appendContent(item.Get("content"))
				case "input_text", "output_text", "text":
					appendText(item.Get("text"))
				case "function_call_output", "custom_tool_call_output":
					appendContent(item.Get("output"))
				}
			}
		}
		appendContent(root.Get("instructions"))
	case ProtocolGemini:
		for _, content := range root.Get("contents").Array() {
			for _, part := range content.Get("parts").Array() {
				appendText(part.Get("text"))
			}
		}
		for _, name := range []string{"systemInstruction", "system_instruction"} {
			for _, part := range root.Get(name + ".parts").Array() {
				appendText(part.Get("text"))
			}
		}
	}
	return strings.Join(texts, "\n")
}
