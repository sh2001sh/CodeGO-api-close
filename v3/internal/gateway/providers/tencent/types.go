package tencent

import "encoding/json"

type nativeUsage struct {
	PromptTokens     *int64 `json:"PromptTokens"`
	CompletionTokens *int64 `json:"CompletionTokens"`
	TotalTokens      *int64 `json:"TotalTokens"`
}

type nativeChoice struct {
	Index        *int          `json:"Index"`
	Message      nativeMessage `json:"Message"`
	Delta        nativeMessage `json:"Delta"`
	FinishReason string        `json:"FinishReason"`
}

type nativeResponse struct {
	ID       string          `json:"Id"`
	Created  int64           `json:"Created"`
	Choices  []nativeChoice  `json:"Choices"`
	Usage    *nativeUsage    `json:"Usage"`
	Error    *nativeError    `json:"Error"`
	Response json.RawMessage `json:"Response"`
}

type nativeError struct {
	Code    json.RawMessage `json:"Code"`
	Message string          `json:"Message"`
}

type chatMessage struct {
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
	Reasoning string `json:"reasoning_content,omitempty"`
}

type chatChoice struct {
	Index        int          `json:"index"`
	Message      *chatMessage `json:"message,omitempty"`
	Delta        *chatMessage `json:"delta,omitempty"`
	FinishReason *string      `json:"finish_reason"`
}

type chatUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
}
