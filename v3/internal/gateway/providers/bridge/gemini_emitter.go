package bridge

import (
	"encoding/json"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type geminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}
type geminiPart struct {
	Text         *string             `json:"text,omitempty"`
	Thought      bool                `json:"thought,omitempty"`
	FunctionCall *geminiFunctionCall `json:"functionCall,omitempty"`
}
type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}
type geminiCandidate struct {
	Index        int            `json:"index"`
	Content      *geminiContent `json:"content,omitempty"`
	FinishReason string         `json:"finishReason,omitempty"`
}
type geminiUsage struct {
	Prompt     int64 `json:"promptTokenCount"`
	Candidates int64 `json:"candidatesTokenCount"`
	Total      int64 `json:"totalTokenCount"`
	Cached     int64 `json:"cachedContentTokenCount,omitempty"`
}
type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
	Usage      *geminiUsage      `json:"usageMetadata,omitempty"`
	Model      string            `json:"modelVersion,omitempty"`
}
type geminiEmitter struct {
	model string
	tools toolCollector
}

func geminiUsageOf(u *gateway.Usage) *geminiUsage {
	if u == nil {
		return nil
	}
	return &geminiUsage{Prompt: u.PromptTokens, Candidates: u.CompletionTokens, Total: u.PromptTokens + u.CompletionTokens, Cached: u.CachedTokens}
}
func geminiStop(reason string) string {
	switch reason {
	case "length":
		return "MAX_TOKENS"
	case "content_filter":
		return "SAFETY"
	default:
		return "STOP"
	}
}
func (g *geminiEmitter) onDelta(delta chatDelta) ([]gateway.Event, error) {
	if err := g.tools.add(delta.ToolCalls); err != nil {
		return nil, err
	}
	var parts []geminiPart
	if delta.ReasoningContent != "" {
		parts = append(parts, geminiPart{Text: pointer(delta.ReasoningContent), Thought: true})
	}
	if delta.Content != "" {
		parts = append(parts, geminiPart{Text: pointer(delta.Content)})
	}
	if delta.Refusal != "" {
		parts = append(parts, geminiPart{Text: pointer(delta.Refusal)})
	}
	if len(parts) == 0 {
		return nil, nil
	}
	response := geminiResponse{Candidates: []geminiCandidate{{Index: 0, Content: &geminiContent{Role: "model", Parts: parts}}}, Model: g.model}
	return []gateway.Event{encodedEvent("", response, len(delta.Content)+len(delta.ReasoningContent)+len(delta.Refusal))}, nil
}
func (g *geminiEmitter) finish(usage *gateway.Usage, reason string) ([]gateway.Event, error) {
	tools, err := g.tools.finish()
	if err != nil {
		return nil, err
	}
	candidate := geminiCandidate{Index: 0, FinishReason: geminiStop(reason)}
	if len(tools) > 0 {
		content := &geminiContent{Role: "model", Parts: []geminiPart{}}
		for _, tool := range tools {
			content.Parts = append(content.Parts, geminiPart{FunctionCall: &geminiFunctionCall{Name: tool.Function.Name, Args: json.RawMessage(tool.Function.Arguments)}})
		}
		candidate.Content = content
	}
	event := encodedEvent("", geminiResponse{Candidates: []geminiCandidate{candidate}, Model: g.model, Usage: geminiUsageOf(usage)}, 0)
	event.Usage = usage
	return []gateway.Event{event}, nil
}
func geminiSingle(model string, delta chatDelta, reason string, usage *gateway.Usage) ([]byte, error) {
	content := &geminiContent{Role: "model", Parts: []geminiPart{}}
	if delta.ReasoningContent != "" {
		content.Parts = append(content.Parts, geminiPart{Text: pointer(delta.ReasoningContent), Thought: true})
	}
	if delta.Content != "" {
		content.Parts = append(content.Parts, geminiPart{Text: pointer(delta.Content)})
	}
	if delta.Refusal != "" {
		content.Parts = append(content.Parts, geminiPart{Text: pointer(delta.Refusal)})
	}
	for _, tool := range delta.ToolCalls {
		args, err := toolArguments(tool.Function.Arguments)
		if err != nil {
			return nil, err
		}
		content.Parts = append(content.Parts, geminiPart{FunctionCall: &geminiFunctionCall{Name: tool.Function.Name, Args: args}})
	}
	return json.Marshal(geminiResponse{Candidates: []geminiCandidate{{Index: 0, Content: content, FinishReason: geminiStop(reason)}}, Model: model, Usage: geminiUsageOf(usage)})
}
