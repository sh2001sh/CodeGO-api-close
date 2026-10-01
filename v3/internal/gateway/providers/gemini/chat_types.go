package gemini

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
}

type chatChoice struct {
	Index   int          `json:"index"`
	Message *chatMessage `json:"message,omitempty"`
	Delta   *chatMessage `json:"delta,omitempty"`
	Finish  *string      `json:"finish_reason"`
}

type chatMessage struct {
	Role      string         `json:"role,omitempty"`
	Content   *string        `json:"content,omitempty"`
	Reasoning string         `json:"reasoning_content,omitempty"`
	Calls     []chatToolCall `json:"tool_calls,omitempty"`
	Extra     *extraContent  `json:"extra_content,omitempty"`
}

type chatToolCall struct {
	Index    *int          `json:"index,omitempty"`
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Function chatFunction  `json:"function"`
	Extra    *extraContent `json:"extra_content,omitempty"`
}

type chatFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type extraContent struct {
	Google googleExtra `json:"google"`
}

type googleExtra struct {
	Signature string `json:"thought_signature"`
}

type chatUsage struct {
	Prompt     int64 `json:"prompt_tokens"`
	Completion int64 `json:"completion_tokens"`
	Total      int64 `json:"total_tokens"`
	Input      struct {
		Cached int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	Output struct {
		Reasoning int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}
