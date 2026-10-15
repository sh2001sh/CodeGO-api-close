package dto

import (
	"encoding/json"

	"github.com/sh2001sh/new-api/types"

	"github.com/gin-gonic/gin"
)

type OpenAIResponsesCompactionRequest struct {
	Model              string          `json:"model"`
	Input              json.RawMessage `json:"input,omitempty"`
	Instructions       json.RawMessage `json:"instructions,omitempty"`
	Tools              json.RawMessage `json:"tools,omitempty"`
	ParallelToolCalls  json.RawMessage `json:"parallel_tool_calls,omitempty"`
	Reasoning          *Reasoning      `json:"reasoning,omitempty"`
	ServiceTier        string          `json:"service_tier,omitempty"`
	PromptCacheKey     json.RawMessage `json:"prompt_cache_key,omitempty"`
	Text               json.RawMessage `json:"text,omitempty"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
}

func (r *OpenAIResponsesCompactionRequest) GetTokenCountMeta() *types.TokenCountMeta {
	if r == nil {
		return &types.TokenCountMeta{}
	}
	// Use the same input/file accounting as ordinary Responses requests. This
	// keeps compaction reservations accurate when history contains images,
	// files, tools, or structured text controls.
	return (&OpenAIResponsesRequest{
		Model:             r.Model,
		Input:             r.Input,
		Instructions:      r.Instructions,
		Tools:             r.Tools,
		Reasoning:         r.Reasoning,
		Text:              r.Text,
		ParallelToolCalls: r.ParallelToolCalls,
	}).GetTokenCountMeta()
}

func (r *OpenAIResponsesCompactionRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *OpenAIResponsesCompactionRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
