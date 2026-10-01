package bridge

import (
	"encoding/json"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type anthropicContent struct {
	Type     string          `json:"type"`
	Text     *string         `json:"text,omitempty"`
	Thinking *string         `json:"thinking,omitempty"`
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
}
type anthropicUsage struct {
	Input      int64 `json:"input_tokens"`
	Output     int64 `json:"output_tokens"`
	CacheRead  int64 `json:"cache_read_input_tokens,omitempty"`
	CacheWrite int64 `json:"cache_creation_input_tokens,omitempty"`
}
type anthropicMessage struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Model        string             `json:"model"`
	Content      []anthropicContent `json:"content"`
	StopReason   *string            `json:"stop_reason"`
	StopSequence *string            `json:"stop_sequence"`
	Usage        anthropicUsage     `json:"usage"`
}
type anthropicDelta struct {
	Type         string  `json:"type,omitempty"`
	Text         *string `json:"text,omitempty"`
	Thinking     *string `json:"thinking,omitempty"`
	PartialJSON  *string `json:"partial_json,omitempty"`
	StopReason   *string `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}
type anthropicEvent struct {
	Type    string            `json:"type"`
	Index   *int              `json:"index,omitempty"`
	Message *anthropicMessage `json:"message,omitempty"`
	Block   *anthropicContent `json:"content_block,omitempty"`
	Delta   *anthropicDelta   `json:"delta,omitempty"`
	Usage   *anthropicUsage   `json:"usage,omitempty"`
}
type anthropicEmitter struct {
	id, model     string
	started, open bool
	index         int
	kind          string
	tools         toolCollector
}

func anthropicUsageOf(u *gateway.Usage) anthropicUsage {
	if u == nil {
		return anthropicUsage{}
	}
	input := u.PromptTokens - u.CachedTokens - u.CacheWriteTokens - u.CacheWrite1hTokens
	if input < 0 {
		input = 0
	}
	return anthropicUsage{Input: input, Output: u.CompletionTokens, CacheRead: u.CachedTokens, CacheWrite: u.CacheWriteTokens + u.CacheWrite1hTokens}
}
func anthropicStop(reason string, tools bool) string {
	if tools || reason == "tool_calls" {
		return "tool_use"
	}
	if reason == "length" {
		return "max_tokens"
	}
	if reason == "stop_sequence" {
		return "stop_sequence"
	}
	if reason == "content_filter" {
		return "refusal"
	}
	return "end_turn"
}
func (a *anthropicEmitter) emit(event anthropicEvent, text int) gateway.Event {
	return encodedEvent(event.Type, event, text)
}
func (a *anthropicEmitter) stopBlock() []gateway.Event {
	if !a.open {
		return nil
	}
	a.open = false
	event := a.emit(anthropicEvent{Type: "content_block_stop", Index: pointer(a.index)}, 0)
	a.index++
	return []gateway.Event{event}
}
func (a *anthropicEmitter) text(kind, text string) []gateway.Event {
	var out []gateway.Event
	if a.open && a.kind != kind {
		out = append(out, a.stopBlock()...)
	}
	if !a.open {
		block := anthropicContent{Type: "text", Text: pointer("")}
		if kind == "thinking" {
			block = anthropicContent{Type: "thinking", Thinking: pointer("")}
		}
		a.open = true
		a.kind = kind
		out = append(out, a.emit(anthropicEvent{Type: "content_block_start", Index: pointer(a.index), Block: &block}, 0))
	}
	delta := anthropicDelta{Type: "text_delta", Text: pointer(text)}
	if kind == "thinking" {
		delta = anthropicDelta{Type: "thinking_delta", Thinking: pointer(text)}
	}
	return append(out, a.emit(anthropicEvent{Type: "content_block_delta", Index: pointer(a.index), Delta: &delta}, len(text)))
}
func (a *anthropicEmitter) onDelta(delta chatDelta) ([]gateway.Event, error) {
	var out []gateway.Event
	if !a.started {
		a.started = true
		message := anthropicMessage{ID: a.id, Type: "message", Role: "assistant", Model: a.model, Content: []anthropicContent{}}
		out = append(out, a.emit(anthropicEvent{Type: "message_start", Message: &message}, 0))
	}
	if delta.ReasoningContent != "" {
		out = append(out, a.text("thinking", delta.ReasoningContent)...)
	}
	if delta.Content != "" {
		out = append(out, a.text("text", delta.Content)...)
	}
	if delta.Refusal != "" {
		out = append(out, a.text("text", delta.Refusal)...)
	}
	if err := a.tools.add(delta.ToolCalls); err != nil {
		return nil, err
	}
	return out, nil
}
func (a *anthropicEmitter) finish(usage *gateway.Usage, reason string) ([]gateway.Event, error) {
	tools, err := a.tools.finish()
	if err != nil {
		return nil, err
	}
	out := a.stopBlock()
	for _, tool := range tools {
		block := anthropicContent{Type: "tool_use", ID: tool.ID, Name: tool.Function.Name, Input: json.RawMessage(`{}`)}
		out = append(out, a.emit(anthropicEvent{Type: "content_block_start", Index: pointer(a.index), Block: &block}, 0))
		delta := anthropicDelta{Type: "input_json_delta", PartialJSON: pointer(tool.Function.Arguments)}
		out = append(out, a.emit(anthropicEvent{Type: "content_block_delta", Index: pointer(a.index), Delta: &delta}, 0), a.emit(anthropicEvent{Type: "content_block_stop", Index: pointer(a.index)}, 0))
		a.index++
	}
	u := anthropicUsageOf(usage)
	delta := anthropicDelta{StopReason: pointer(anthropicStop(reason, len(tools) > 0))}
	event := a.emit(anthropicEvent{Type: "message_delta", Delta: &delta, Usage: &u}, 0)
	event.Usage = usage
	out = append(out, event, a.emit(anthropicEvent{Type: "message_stop"}, 0))
	return out, nil
}
func anthropicSingle(id, model string, delta chatDelta, reason string, usage *gateway.Usage) ([]byte, error) {
	m := anthropicMessage{ID: id, Type: "message", Role: "assistant", Model: model, Content: []anthropicContent{}, StopReason: pointer(anthropicStop(reason, len(delta.ToolCalls) > 0)), Usage: anthropicUsageOf(usage)}
	if delta.ReasoningContent != "" {
		m.Content = append(m.Content, anthropicContent{Type: "thinking", Thinking: pointer(delta.ReasoningContent)})
	}
	if delta.Content != "" {
		m.Content = append(m.Content, anthropicContent{Type: "text", Text: pointer(delta.Content)})
	}
	if delta.Refusal != "" {
		m.Content = append(m.Content, anthropicContent{Type: "text", Text: pointer(delta.Refusal)})
	}
	for _, tool := range delta.ToolCalls {
		args, err := toolArguments(tool.Function.Arguments)
		if err != nil {
			return nil, err
		}
		m.Content = append(m.Content, anthropicContent{Type: "tool_use", ID: tool.ID, Name: tool.Function.Name, Input: args})
	}
	return json.Marshal(m)
}
