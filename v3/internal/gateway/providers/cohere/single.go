package cohere

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func (s *responseStream) single(root gjson.Result) (gateway.Event, error) {
	usage, err := parseUsage(root.Get("usage"))
	if err != nil {
		return gateway.Event{}, err
	}
	reason := root.Get("finish_reason").Str
	if failedReason(reason) {
		return gateway.Event{Kind: gateway.EventError, Err: generationError(reason), Usage: usage}, nil
	}
	if reason == "" {
		return gateway.Event{Kind: gateway.EventError, Err: upstream("invalid_response", "cohere: missing finish reason")}, nil
	}
	message := chatMessage{Role: "assistant", Reasoning: root.Get("message.tool_plan").Str}
	for _, part := range root.Get("message.content").Array() {
		if part.Get("type").Str != "text" {
			return gateway.Event{Kind: gateway.EventError, Err: upstream("unsupported_output", "cohere: non-text output is unsupported")}, nil
		}
		message.Content += part.Get("text").Str
	}
	textBytes := len(message.Content) + len(message.Reasoning)
	for _, call := range root.Get("message.tool_calls").Array() {
		c, err := parseCall(call)
		if err != nil {
			return gateway.Event{}, err
		}
		if c.ID == "" || c.Function.Name == "" || !gjson.Valid(c.Function.Arguments) || !gjson.Parse(c.Function.Arguments).IsObject() {
			return gateway.Event{}, errors.New("cohere: invalid completed tool call")
		}
		textBytes += len(c.Function.Arguments)
		message.ToolCalls = append(message.ToolCalls, c)
	}
	if message.Content == "" && message.Reasoning == "" && len(message.ToolCalls) == 0 {
		return gateway.Event{Kind: gateway.EventError, Err: upstream("empty_response", "Cohere returned no output"), Usage: usage}, nil
	}
	if id := root.Get("id").Str; id != "" {
		s.id = id
	}
	finish := finishReason(reason, len(message.ToolCalls) > 0)
	payload, err := json.Marshal(chatResponse{ID: s.id, Object: "chat.completion", Created: s.created, Model: s.model,
		Choices: []chatChoice{{Message: &message, FinishReason: &finish}}, Usage: usageJSON(usage)})
	return gateway.Event{Kind: gateway.EventData, Payload: payload, TextBytes: textBytes, Usage: usage}, err
}

func parseCall(root gjson.Result) (toolCall, error) {
	if !root.IsObject() {
		return toolCall{}, errors.New("cohere: invalid tool event")
	}
	c := toolCall{ID: root.Get("id").Str, Type: root.Get("type").Str,
		Function: callFunction{Name: root.Get("function.name").Str, Arguments: root.Get("function.arguments").Str}}
	if c.Type != "" && c.Type != "function" {
		return c, errors.New("cohere: unsupported tool type")
	}
	if args := root.Get("function.arguments"); args.Exists() && args.Type != gjson.String {
		return c, errors.New("cohere: tool arguments must be a JSON string")
	}
	return c, nil
}

func parseUsage(root gjson.Result) (*gateway.Usage, error) {
	billed := root.Get("billed_units")
	if !billed.IsObject() {
		billed = root.Get("tokens")
	}
	input, hasInput, err := tokenCount(billed.Get("input_tokens"))
	if err != nil {
		return nil, err
	}
	output, hasOutput, err := tokenCount(billed.Get("output_tokens"))
	if err != nil {
		return nil, err
	}
	if !hasInput || !hasOutput {
		return nil, nil
	}
	return &gateway.Usage{PromptTokens: input, CompletionTokens: output}, nil
}

func tokenCount(value gjson.Result) (int64, bool, error) {
	if !value.Exists() || value.Type == gjson.Null {
		return 0, false, nil
	}
	if value.Type != gjson.Number {
		return 0, false, errors.New("cohere: token count must be an integer")
	}
	count, err := strconv.ParseInt(value.Raw, 10, 64)
	if err != nil || count < 0 {
		return 0, false, errors.New("cohere: invalid token count")
	}
	return count, true, nil
}

func usageJSON(usage *gateway.Usage) *chatUsage {
	if usage == nil {
		return nil
	}
	return &chatUsage{PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, TotalTokens: usage.PromptTokens + usage.CompletionTokens}
}

func finishReason(reason string, tools bool) string {
	if tools {
		return "tool_calls"
	}
	switch strings.ToUpper(reason) {
	case "MAX_TOKENS":
		return "length"
	case "ERROR_TOXIC":
		return "content_filter"
	default:
		return "stop"
	}
}

func failedReason(reason string) bool { return reason == "ERROR" || reason == "ERROR_LIMIT" }
func generationError(reason string) *gateway.UpstreamError {
	return upstream("cohere_generation_error", "Cohere generation failed: "+reason)
}

func responseError(root gjson.Result) *gateway.UpstreamError {
	e := root.Get("error")
	message := root.Get("message")
	if (!e.Exists() || e.Type == gjson.Null) && root.Get("type").Str != "error" && message.Type != gjson.String {
		return nil
	}
	text := e.Get("message").Str
	if text == "" {
		text = message.Str
	}
	if text == "" && e.Type == gjson.String {
		text = e.Str
	}
	if text == "" {
		text = "Cohere reported an error"
	}
	return upstream("cohere_error", text)
}
