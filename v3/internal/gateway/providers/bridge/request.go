package bridge

import (
	"encoding/json"
	"fmt"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type chatRequest struct {
	Model             string          `json:"model"`
	Messages          []chatMessage   `json:"messages"`
	Stream            bool            `json:"stream"`
	MaxTokens         *int64          `json:"max_tokens,omitempty"`
	Temperature       *float64        `json:"temperature,omitempty"`
	TopP              *float64        `json:"top_p,omitempty"`
	Stop              json.RawMessage `json:"stop,omitempty"`
	Tools             []chatTool      `json:"tools,omitempty"`
	ToolChoice        json.RawMessage `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
	ReasoningEffort   string          `json:"reasoning_effort,omitempty"`
	ResponseFormat    json.RawMessage `json:"response_format,omitempty"`
	StreamOptions     *streamOptions  `json:"stream_options,omitempty"`
}
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}
type chatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	ToolCalls  []chatToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}
type chatTool struct {
	Type     string       `json:"type"`
	Function functionSpec `json:"function"`
}
type functionSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}
type chatToolCall struct {
	Index    int          `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function functionCall `json:"function"`
}
type functionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}
type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}
type imageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

func normalize(req *gateway.Request) (*gateway.Request, error) {
	if !gjson.ValidBytes(req.Body) {
		return nil, fmt.Errorf("bridge: invalid request JSON")
	}
	root := gjson.ParseBytes(req.Body)
	c := chatRequest{Model: req.Model, Stream: req.Stream}
	var err error
	switch req.Protocol {
	case gateway.ProtocolResponses:
		err = responsesRequest(root, &c)
	case gateway.ProtocolAnthropic:
		err = anthropicRequest(root, &c)
	case gateway.ProtocolGemini:
		err = geminiRequest(root, &c)
	default:
		err = fmt.Errorf("bridge: unsupported client protocol %d", req.Protocol)
	}
	if err != nil {
		return nil, err
	}
	if len(c.Messages) == 0 {
		return nil, fmt.Errorf("bridge: request contains no messages")
	}
	if c.Stream {
		c.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	body, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	copy := *req
	copy.Protocol = gateway.ProtocolOpenAIChat
	copy.Body = body
	return &copy, nil
}

func allowedFields(root gjson.Result, allowed string) error {
	fields := make(map[string]bool)
	for _, k := range splitFields(allowed) {
		fields[k] = true
	}
	var err error
	root.ForEach(func(key, value gjson.Result) bool {
		if !fields[key.Str] {
			err = fmt.Errorf("bridge: field %q cannot be converted losslessly to Chat", key.Str)
			return false
		}
		return true
	})
	return err
}
func splitFields(s string) []string { // field names contain no whitespace
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ' ' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}
func raw(v gjson.Result) json.RawMessage {
	if !v.Exists() {
		return nil
	}
	return json.RawMessage(v.Raw)
}
func textContent(s string) json.RawMessage { b, _ := json.Marshal(s); return b }
func intOption(v gjson.Result) *int64 {
	if !v.Exists() {
		return nil
	}
	n := v.Int()
	return &n
}
func floatOption(v gjson.Result) *float64 {
	if !v.Exists() {
		return nil
	}
	n := v.Float()
	return &n
}
func boolOption(v gjson.Result) *bool {
	if !v.Exists() {
		return nil
	}
	n := v.Bool()
	return &n
}
func appendParts(message *chatMessage, parts []contentPart) {
	if len(parts) > 0 {
		message.Content, _ = json.Marshal(parts)
	}
}
func appendText(parts *[]contentPart, s string) {
	*parts = append(*parts, contentPart{Type: "text", Text: s})
}
