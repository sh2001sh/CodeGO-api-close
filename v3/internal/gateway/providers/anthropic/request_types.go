package anthropic

import "encoding/json"

type chatRequest struct {
	Messages            []chatMessage   `json:"messages"`
	Stream              bool            `json:"stream"`
	MaxTokens           *int64          `json:"max_tokens"`
	MaxCompletionTokens *int64          `json:"max_completion_tokens"`
	Temperature         *float64        `json:"temperature"`
	TopP                *float64        `json:"top_p"`
	Stop                json.RawMessage `json:"stop"`
	Tools               []chatTool      `json:"tools"`
	ToolChoice          json.RawMessage `json:"tool_choice"`
	ParallelToolCalls   *bool           `json:"parallel_tool_calls"`
	User                string          `json:"user"`
}

type chatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCalls  []chatToolCall  `json:"tool_calls"`
	ToolCallID string          `json:"tool_call_id"`
}

type chatTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
		Strict      *bool           `json:"strict"`
	} `json:"function"`
}

type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type messagesRequest struct {
	Model       string      `json:"model"`
	Messages    []message   `json:"messages"`
	System      []block     `json:"system,omitempty"`
	Stream      bool        `json:"stream"`
	MaxTokens   int64       `json:"max_tokens"`
	Temperature *float64    `json:"temperature,omitempty"`
	TopP        *float64    `json:"top_p,omitempty"`
	Stop        []string    `json:"stop_sequences,omitempty"`
	Tools       []tool      `json:"tools,omitempty"`
	ToolChoice  *toolChoice `json:"tool_choice,omitempty"`
	Metadata    *metadata   `json:"metadata,omitempty"`
}

type metadata struct {
	UserID string `json:"user_id"`
}
type message struct {
	Role    string  `json:"role"`
	Content []block `json:"content"`
}
type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Source    *source         `json:"source,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}
type source struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}
type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}
type toolChoice struct {
	Type            string `json:"type"`
	Name            string `json:"name,omitempty"`
	DisableParallel *bool  `json:"disable_parallel_tool_use,omitempty"`
}
