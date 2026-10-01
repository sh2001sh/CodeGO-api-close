package bridge

import (
	"fmt"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// Finish emits every paired done event before the one final response lifecycle.
// Repeated calls emit nothing, so replaying a transport terminator is harmless.
func (e *ResponsesEmitter) Finish(usage *gateway.Usage, reason string) ([]gateway.Event, error) {
	if e.done {
		return nil, nil
	}
	if !e.started {
		e.done = true
		return nil, nil
	}
	if err := e.finalizeToolItems(); err != nil {
		return nil, err
	}
	e.done = true
	var out []gateway.Event
	for index := range e.items {
		out = append(out, e.finishItem(index)...)
	}
	return append(out, e.terminalEvent(usage, reason)), nil
}

// finalizeToolItems copies each buffered tool call's accumulated arguments,
// name and ID onto its output item before the done events are emitted.
func (e *ResponsesEmitter) finalizeToolItems() error {
	for _, state := range e.tools {
		if state.outputIndex < 0 {
			return fmt.Errorf("bridge: incomplete tool-call metadata")
		}
		if _, err := toolArguments(state.arguments.String()); err != nil {
			return err
		}
		item := &e.items[state.outputIndex]
		arguments := state.arguments.String()
		if arguments == "" {
			arguments = "{}"
		}
		item.Arguments = pointer(arguments)
		item.Name = state.name
		item.CallID = state.id
	}
	return nil
}

// finishItem emits the type-specific done event(s) for one output item
// followed by its response.output_item.done event.
func (e *ResponsesEmitter) finishItem(index int) []gateway.Event {
	item := &e.items[index]
	item.Status = "completed"
	event := responseEvent{ItemID: item.ID, OutputIndex: pointer(index)}
	var out []gateway.Event
	switch item.Type {
	case "function_call":
		event.Type = "response.function_call_arguments.done"
		event.Arguments = item.Arguments
		out = append(out, e.emit(event, 0))
	case "reasoning":
		item.Summary[0].Text = e.reasoning.String()
		part := item.Summary[0]
		event.SummaryIndex = pointer(0)
		event.Type = "response.reasoning_summary_text.done"
		event.Text = pointer(part.Text)
		out = append(out, e.emit(event, 0))
		event.Type = "response.reasoning_summary_part.done"
		event.Text = nil
		event.Part = &part
		out = append(out, e.emit(event, 0))
	case "message":
		if item.Content[0].Type == "refusal" {
			item.Content[0].Refusal = e.refusal.String()
		} else {
			item.Content[0].Text = e.text.String()
		}
		part := item.Content[0]
		event.ContentIndex = pointer(0)
		if part.Type == "refusal" {
			event.Type = "response.refusal.done"
			event.Refusal = pointer(part.Refusal)
		} else {
			event.Type = "response.output_text.done"
			event.Text = pointer(part.Text)
		}
		out = append(out, e.emit(event, 0))
		event.Type = "response.content_part.done"
		event.Text = nil
		event.Refusal = nil
		event.Part = &part
		out = append(out, e.emit(event, 0))
	}
	out = append(out, e.emit(responseEvent{Type: "response.output_item.done", OutputIndex: pointer(index), Item: item}, 0))
	return out
}

// terminalEvent builds the final response.completed/response.incomplete event.
func (e *ResponsesEmitter) terminalEvent(usage *gateway.Usage, reason string) gateway.Event {
	r := &responseWire{ID: e.id, Object: "response", Status: "completed", Model: e.model, Output: e.items, Usage: responseUsageOf(usage)}
	terminal := "response.completed"
	if reason == "length" || reason == "content_filter" {
		r.Status = "incomplete"
		r.IncompleteDetails = &incompleteDetails{Reason: responseIncompleteReason(reason)}
		terminal = "response.incomplete"
	}
	event := e.emit(responseEvent{Type: terminal, Response: r}, 0)
	event.Usage = usage
	return event
}
