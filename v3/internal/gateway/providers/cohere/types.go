package cohere

type callFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

type toolCall struct {
	Index    *int         `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function callFunction `json:"function"`
}

type chatMessage struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	Reasoning string     `json:"reasoning_content,omitempty"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
}

type chatUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

type chatChoice struct {
	Index        int          `json:"index"`
	Message      *chatMessage `json:"message,omitempty"`
	Delta        *chatMessage `json:"delta,omitempty"`
	FinishReason *string      `json:"finish_reason"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
}
