package dto

import "bytes"

// Clone preserves the request's raw JSON and optional values while giving
// channel-specific rewrites their own mutable storage.
func (r *OpenAIResponsesRequest) Clone() *OpenAIResponsesRequest {
	if r == nil {
		return nil
	}
	cloned := *r
	cloned.Input = bytes.Clone(r.Input)
	cloned.Include = bytes.Clone(r.Include)
	cloned.Conversation = bytes.Clone(r.Conversation)
	cloned.ContextManagement = bytes.Clone(r.ContextManagement)
	cloned.Instructions = bytes.Clone(r.Instructions)
	cloned.Metadata = bytes.Clone(r.Metadata)
	cloned.ClientMetadata = bytes.Clone(r.ClientMetadata)
	cloned.ParallelToolCalls = bytes.Clone(r.ParallelToolCalls)
	cloned.Store = bytes.Clone(r.Store)
	cloned.PromptCacheKey = bytes.Clone(r.PromptCacheKey)
	cloned.PromptCacheRetention = bytes.Clone(r.PromptCacheRetention)
	cloned.SafetyIdentifier = bytes.Clone(r.SafetyIdentifier)
	cloned.Text = bytes.Clone(r.Text)
	cloned.ToolChoice = bytes.Clone(r.ToolChoice)
	cloned.Tools = bytes.Clone(r.Tools)
	cloned.Truncation = bytes.Clone(r.Truncation)
	cloned.User = bytes.Clone(r.User)
	cloned.Prompt = bytes.Clone(r.Prompt)
	cloned.EnableThinking = bytes.Clone(r.EnableThinking)
	cloned.Preset = bytes.Clone(r.Preset)
	cloned.Background = cloneResponsesScalar(r.Background)
	cloned.MaxOutputTokens = cloneResponsesScalar(r.MaxOutputTokens)
	cloned.TopLogProbs = cloneResponsesScalar(r.TopLogProbs)
	cloned.Stream = cloneResponsesScalar(r.Stream)
	cloned.Temperature = cloneResponsesScalar(r.Temperature)
	cloned.FrequencyPenalty = cloneResponsesScalar(r.FrequencyPenalty)
	cloned.PresencePenalty = cloneResponsesScalar(r.PresencePenalty)
	cloned.TopP = cloneResponsesScalar(r.TopP)
	cloned.MaxToolCalls = cloneResponsesScalar(r.MaxToolCalls)
	if r.Reasoning != nil {
		reasoning := *r.Reasoning
		cloned.Reasoning = &reasoning
	}
	if r.StreamOptions != nil {
		options := *r.StreamOptions
		cloned.StreamOptions = &options
	}
	return &cloned
}

func cloneResponsesScalar[T bool | uint | int | float64](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
