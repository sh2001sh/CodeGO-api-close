package gateway

import "encoding/json"

// These response shapes match the legacy force_format projection. They are
// intentionally local: v3 must not import v2 DTOs or its platform dependencies.
func forceResponseFormat(payload []byte, streaming bool) ([]byte, error) {
	if streaming {
		var response formattedChatStream
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, err
		}
		return json.Marshal(response)
	}
	var response formattedChatResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, err
	}
	return json.Marshal(response)
}

type formattedChatStream struct {
	ID                string                `json:"id"`
	Object            string                `json:"object"`
	Created           int64                 `json:"created"`
	Model             string                `json:"model"`
	SystemFingerprint *string               `json:"system_fingerprint"`
	Choices           []formattedChatChoice `json:"choices"`
	Usage             *formattedChatUsage   `json:"usage"`
}

type formattedChatChoice struct {
	Delta        formattedChatDelta `json:"delta,omitempty"`
	Logprobs     *json.RawMessage   `json:"logprobs"`
	FinishReason *string            `json:"finish_reason"`
	Index        int                `json:"index"`
}

type formattedChatDelta struct {
	Content          *string             `json:"content,omitempty"`
	ReasoningContent *string             `json:"reasoning_content,omitempty"`
	Reasoning        *string             `json:"reasoning,omitempty"`
	Role             string              `json:"role,omitempty"`
	ToolCalls        []formattedChatTool `json:"tool_calls,omitempty"`
}

type formattedChatTool struct {
	Index    *int                  `json:"index,omitempty"`
	ID       string                `json:"id,omitempty"`
	Type     any                   `json:"type"`
	Function formattedChatFunction `json:"function"`
}

type formattedChatFunction struct {
	Description string `json:"description,omitempty"`
	Name        string `json:"name,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
	Arguments   string `json:"arguments"`
}

type formattedChatResponse struct {
	ID      string                        `json:"id"`
	Model   string                        `json:"model"`
	Object  string                        `json:"object"`
	Created any                           `json:"created"`
	Choices []formattedChatResponseChoice `json:"choices"`
	Error   any                           `json:"error,omitempty"`
	Usage   formattedChatUsage            `json:"usage"`
}

type formattedChatResponseChoice struct {
	Index        int                  `json:"index"`
	Message      formattedChatMessage `json:"message"`
	FinishReason string               `json:"finish_reason"`
}

type formattedChatMessage struct {
	Role             string          `json:"role"`
	Content          any             `json:"content"`
	Name             *string         `json:"name,omitempty"`
	Prefix           *bool           `json:"prefix,omitempty"`
	ReasoningContent *string         `json:"reasoning_content,omitempty"`
	Reasoning        *string         `json:"reasoning,omitempty"`
	ToolCalls        json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
}

type formattedChatUsage struct {
	PromptTokens         int    `json:"prompt_tokens"`
	CompletionTokens     int    `json:"completion_tokens"`
	TotalTokens          int    `json:"total_tokens"`
	PromptCacheHitTokens int    `json:"prompt_cache_hit_tokens,omitempty"`
	UsageSemantic        string `json:"usage_semantic,omitempty"`
	UsageSource          string `json:"usage_source,omitempty"`

	PromptTokensDetails    formattedInputDetails  `json:"prompt_tokens_details"`
	CompletionTokenDetails formattedOutputDetails `json:"completion_tokens_details"`
	InputTokens            int                    `json:"input_tokens"`
	OutputTokens           int                    `json:"output_tokens"`
	InputTokensDetails     *formattedInputDetails `json:"input_tokens_details"`

	ClaudeCacheCreation5mTokens int `json:"claude_cache_creation_5_m_tokens"`
	ClaudeCacheCreation1hTokens int `json:"claude_cache_creation_1_h_tokens"`
	Cost                        any `json:"cost,omitempty"`
}

type formattedInputDetails struct {
	CachedTokens             int `json:"cached_tokens"`
	CachedCreationTokens     int `json:"cached_creation_tokens,omitempty"`
	CacheCreationTokens      int `json:"cache_creation_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheWriteTokens         int `json:"cache_write_tokens,omitempty"`
	CacheWriteInputTokens    int `json:"cache_write_input_tokens,omitempty"`
	TextTokens               int `json:"text_tokens"`
	AudioTokens              int `json:"audio_tokens"`
	ImageTokens              int `json:"image_tokens"`
}

type formattedOutputDetails struct {
	TextTokens      int `json:"text_tokens"`
	AudioTokens     int `json:"audio_tokens"`
	ImageTokens     int `json:"image_tokens"`
	ReasoningTokens int `json:"reasoning_tokens"`
}
