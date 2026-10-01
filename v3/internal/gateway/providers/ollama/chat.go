package ollama

type chatFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatCall struct {
	Index    *int         `json:"index,omitempty"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatDelta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	Reasoning string     `json:"reasoning_content,omitempty"`
	ToolCalls []chatCall `json:"tool_calls,omitempty"`
}

type chatOutputMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Reasoning string     `json:"reasoning_content,omitempty"`
	ToolCalls []chatCall `json:"tool_calls,omitempty"`
}

type chunkChoice struct {
	Index        int       `json:"index"`
	Delta        chatDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

type outputChoice struct {
	Index        int               `json:"index"`
	Message      chatOutputMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type chatUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

type chatChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []chunkChoice `json:"choices"`
	Usage   *chatUsage    `json:"usage,omitempty"`
}

type chatCompletion struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []outputChoice `json:"choices"`
	Usage   *chatUsage     `json:"usage,omitempty"`
}
