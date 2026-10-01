package ollama

import "encoding/json"

type chatRequest struct {
	Messages            []chatMessage   `json:"messages"`
	MaxTokens           *int64          `json:"max_tokens"`
	MaxCompletionTokens *int64          `json:"max_completion_tokens"`
	Temperature         *float64        `json:"temperature"`
	TopP                *float64        `json:"top_p"`
	TopK                *int            `json:"top_k"`
	FrequencyPenalty    *float64        `json:"frequency_penalty"`
	PresencePenalty     *float64        `json:"presence_penalty"`
	Seed                *int64          `json:"seed"`
	Stop                json.RawMessage `json:"stop"`
	Tools               []tool          `json:"tools"`
	ToolChoice          json.RawMessage `json:"tool_choice"`
	ResponseFormat      *responseFormat `json:"response_format"`
	Think               json.RawMessage `json:"think"`
	KeepAlive           json.RawMessage `json:"keep_alive"`
}

type chatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  []chatToolCall  `json:"tool_calls"`
}

type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type tool struct {
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type responseFormat struct {
	Type       string `json:"type"`
	JSONSchema *struct {
		Schema json.RawMessage `json:"schema"`
		Strict bool            `json:"strict"`
	} `json:"json_schema"`
}

type nativeRequest struct {
	Model     string          `json:"model"`
	Messages  []nativeMessage `json:"messages"`
	Stream    bool            `json:"stream"`
	Options   map[string]any  `json:"options,omitempty"`
	Tools     []tool          `json:"tools,omitempty"`
	Format    any             `json:"format,omitempty"`
	Think     json.RawMessage `json:"think,omitempty"`
	KeepAlive json.RawMessage `json:"keep_alive,omitempty"`
}

type nativeMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Thinking  string           `json:"thinking,omitempty"`
	Images    []string         `json:"images,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
	ToolCalls []nativeToolCall `json:"tool_calls,omitempty"`
}

type nativeToolCall struct {
	Function nativeToolFunction `json:"function"`
}

type nativeToolFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type nativeResponse struct {
	Model           string        `json:"model"`
	CreatedAt       string        `json:"created_at"`
	Message         nativeMessage `json:"message"`
	Done            bool          `json:"done"`
	DoneReason      string        `json:"done_reason"`
	PromptEvalCount *int64        `json:"prompt_eval_count"`
	EvalCount       *int64        `json:"eval_count"`
	Error           string        `json:"error"`
}
