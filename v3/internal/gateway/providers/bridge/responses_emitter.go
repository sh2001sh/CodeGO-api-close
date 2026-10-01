package bridge

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// ResponsesEmitter owns one response's monotonically increasing event sequence,
// output item indices and paired lifecycle events. It is never shared by requests.
type ResponsesEmitter struct {
	id, model                               string
	sequence                                int
	started, done                           bool
	items                                   []responseItem
	textIndex, reasoningIndex, refusalIndex int
	tools                                   map[int]*responseToolState
	text, reasoning, refusal                strings.Builder
}
type responseToolState struct {
	outputIndex int
	id, name    string
	arguments   strings.Builder
}

func NewResponsesEmitter(id, model string) *ResponsesEmitter {
	return &ResponsesEmitter{id: id, model: model, textIndex: -1, reasoningIndex: -1, refusalIndex: -1, tools: make(map[int]*responseToolState)}
}

// OnEvent accepts a decoded Chat data event. Errors and terminal events remain
// the caller's responsibility; Finish emits a terminal native lifecycle once.
func (e *ResponsesEmitter) OnEvent(event gateway.Event) ([]gateway.Event, error) {
	if event.Kind != gateway.EventData {
		return nil, nil
	}
	var frame chatFrame
	if err := json.Unmarshal(event.Payload, &frame); err != nil {
		return nil, err
	}
	if len(frame.Choices) != 1 {
		return nil, fmt.Errorf("bridge: Responses emitter needs one Chat choice")
	}
	var delta chatDelta
	if frame.Choices[0].Delta != nil {
		delta = *frame.Choices[0].Delta
	} else if frame.Choices[0].Message != nil {
		delta = *frame.Choices[0].Message
		for index := range delta.ToolCalls {
			delta.ToolCalls[index].Index = index
		}
	}
	if delta.ReasoningContent == "" {
		delta.ReasoningContent = delta.Reasoning
	}
	events, err := e.onDelta(delta)
	if err != nil {
		return nil, err
	}
	return accountEvents(events, event, delta, event.Usage), nil
}

func (e *ResponsesEmitter) emit(v responseEvent, textBytes int) gateway.Event {
	v.Sequence = e.sequence
	e.sequence++
	return encodedEvent(v.Type, v, textBytes)
}
func (e *ResponsesEmitter) begin() []gateway.Event {
	if e.started {
		return nil
	}
	e.started = true
	r := &responseWire{ID: e.id, Object: "response", Status: "in_progress", Model: e.model, Output: []responseItem{}}
	return []gateway.Event{e.emit(responseEvent{Type: "response.created", Response: r}, 0), e.emit(responseEvent{Type: "response.in_progress", Response: r}, 0)}
}
func (e *ResponsesEmitter) addItem(item responseItem) (int, gateway.Event) {
	index := len(e.items)
	e.items = append(e.items, item)
	return index, e.emit(responseEvent{Type: "response.output_item.added", OutputIndex: pointer(index), Item: &item}, 0)
}

func (e *ResponsesEmitter) onDelta(delta chatDelta) ([]gateway.Event, error) {
	if e.done {
		return nil, fmt.Errorf("bridge: data after Responses completion")
	}
	if delta.Content == "" && delta.ReasoningContent == "" && delta.Refusal == "" && len(delta.ToolCalls) == 0 {
		return nil, nil
	}
	out := e.begin()
	if delta.ReasoningContent != "" {
		out = append(out, e.textDelta(&e.reasoningIndex, "reasoning", delta.ReasoningContent)...)
	}
	if delta.Content != "" {
		out = append(out, e.textDelta(&e.textIndex, "text", delta.Content)...)
	}
	if delta.Refusal != "" {
		out = append(out, e.textDelta(&e.refusalIndex, "refusal", delta.Refusal)...)
	}
	for _, call := range delta.ToolCalls {
		if call.Index < 0 {
			return nil, fmt.Errorf("bridge: negative tool-call index")
		}
		state := e.tools[call.Index]
		if state == nil {
			state = &responseToolState{outputIndex: -1}
			e.tools[call.Index] = state
		}
		if call.ID != "" {
			state.id = call.ID
		}
		state.name += call.Function.Name
		if state.outputIndex < 0 && state.name != "" {
			id := state.id
			if id == "" {
				id = fmt.Sprintf("%s_call_%d", e.id, call.Index)
			}
			state.id = id
			item := responseItem{ID: fmt.Sprintf("%s_tool_%d", e.id, call.Index), Type: "function_call", Status: "in_progress", CallID: id, Name: state.name, Arguments: pointer("")}
			var event gateway.Event
			state.outputIndex, event = e.addItem(item)
			out = append(out, event)
			if state.arguments.Len() > 0 {
				out = append(out, e.argumentDelta(state, state.arguments.String()))
			}
		}
		state.arguments.WriteString(call.Function.Arguments)
		if state.outputIndex >= 0 && call.Function.Arguments != "" {
			out = append(out, e.argumentDelta(state, call.Function.Arguments))
		}
	}
	return out, nil
}
func (e *ResponsesEmitter) argumentDelta(s *responseToolState, delta string) gateway.Event {
	return e.emit(responseEvent{Type: "response.function_call_arguments.delta", ItemID: e.items[s.outputIndex].ID, OutputIndex: pointer(s.outputIndex), Delta: pointer(delta)}, 0)
}

func (e *ResponsesEmitter) textDelta(index *int, kind, text string) []gateway.Event {
	var out []gateway.Event
	part := responsePart{Type: "output_text"}
	item := responseItem{Type: "message", Status: "in_progress", Role: "assistant", Content: []responsePart{part}}
	partAdded, deltaEvent := "response.content_part.added", "response.output_text.delta"
	if kind == "reasoning" {
		part.Type = "summary_text"
		item = responseItem{Type: "reasoning", Status: "in_progress", Summary: []responsePart{part}}
		partAdded = "response.reasoning_summary_part.added"
		deltaEvent = "response.reasoning_summary_text.delta"
	}
	if kind == "refusal" {
		part.Type = "refusal"
		item.Content = []responsePart{part}
		deltaEvent = "response.refusal.delta"
	}
	if *index < 0 {
		item.ID = fmt.Sprintf("%s_%s", e.id, kind)
		var added gateway.Event
		*index, added = e.addItem(item)
		out = append(out, added)
		event := responseEvent{Type: partAdded, ItemID: item.ID, OutputIndex: pointer(*index), Part: &part}
		if kind == "reasoning" {
			event.SummaryIndex = pointer(0)
		} else {
			event.ContentIndex = pointer(0)
		}
		out = append(out, e.emit(event, 0))
	}
	current := &e.items[*index]
	switch kind {
	case "reasoning":
		e.reasoning.WriteString(text)
	case "refusal":
		e.refusal.WriteString(text)
	default:
		e.text.WriteString(text)
	}
	event := responseEvent{Type: deltaEvent, ItemID: current.ID, OutputIndex: pointer(*index), Delta: pointer(text)}
	if kind == "reasoning" {
		event.SummaryIndex = pointer(0)
	} else {
		event.ContentIndex = pointer(0)
	}
	return append(out, e.emit(event, len(text)))
}
