package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

type stream struct {
	body      io.ReadCloser
	reader    *sse.Reader
	chat      bool
	wantUsage bool
	model, id string
	created   int64
	usage     usageState
	tools     map[int]int
	toolCount int
	stopped   bool
	done      bool
	finished  bool
	semantic  bool
}

func (s *stream) Next() (gateway.Event, error) {
	s.semantic = false
	if s.stopped {
		return s.end()
	}
	for {
		ev, err := s.reader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return gateway.Event{}, err
		}
		event, cont, err := s.nextEvent(ev)
		if cont {
			continue
		}
		return event, err
	}
}

// nextEvent decodes and handles one raw SSE event. cont is true when Next's
// loop should read the next event without returning (e.g. a ping, or a
// buffered native event in native passthrough mode that produced no Chat
// output yet).
func (s *stream) nextEvent(ev sse.Event) (gateway.Event, bool, error) {
	var w wireEvent
	if err := json.Unmarshal(ev.Data, &w); err != nil {
		return decodeError(fmt.Errorf("anthropic: invalid SSE JSON: %w", err)), false, nil
	}
	if w.Error != nil {
		return gateway.Event{Kind: gateway.EventError, Err: upstreamError(w.Error)}, false, nil
	}
	if w.Type == "ping" {
		return gateway.Event{}, true, nil
	}
	usage, err := s.usage.merge(w.Usage)
	if w.Type == "message_start" {
		s.id = w.Message.ID
		usage, err = s.usage.merge(w.Message.Usage)
	}
	if err != nil {
		return decodeError(err), false, nil
	}
	if w.Type == "message_stop" {
		s.stopped = true
	}
	if s.chat {
		out, emit, err := s.translate(w, usage)
		if err != nil {
			return decodeError(err), false, nil
		}
		if emit {
			s.semantic = semanticOutput(w, true)
			return out, false, nil
		}
		if s.stopped {
			event, err := s.end()
			return event, false, err
		}
		return gateway.Event{}, true, nil
	}
	name := string(ev.Name)
	if name == "" {
		name = w.Type
	}
	if w.Type == "" {
		return decodeError(fmt.Errorf("anthropic: SSE event lacks type")), false, nil
	}
	textBytes := len(w.Delta.Text) + len(w.Delta.Thinking) + len(w.Delta.PartialJSON)
	textBytes += len(w.ContentBlock.Text) + len(w.ContentBlock.Thinking)
	s.semantic = semanticOutput(w, false)
	return gateway.Event{Kind: gateway.EventData, Name: name, Payload: ev.Data, Usage: usage, TextBytes: textBytes}, false, nil
}

func (s *stream) Close() error { return s.body.Close() }

func (s *stream) end() (gateway.Event, error) {
	if s.done {
		return gateway.Event{}, io.EOF
	}
	s.done = true
	return gateway.Event{Kind: gateway.EventDone}, nil
}

func (s *stream) translate(w wireEvent, usage *gateway.Usage) (gateway.Event, bool, error) {
	var delta chatDelta
	var reason *string
	textBytes := 0
	switch w.Type {
	case "message_start":
		delta.Role = "assistant"
		empty := ""
		delta.Content = &empty
	case "content_block_start":
		d, tb, ok, err := s.blockStartDelta(w)
		if err != nil || !ok {
			return gateway.Event{}, false, err
		}
		delta, textBytes = d, tb
	case "content_block_delta":
		d, tb, ok, err := s.blockDeltaDelta(w)
		if err != nil || !ok {
			return gateway.Event{}, false, err
		}
		delta, textBytes = d, tb
	case "message_delta":
		if w.Delta.StopReason == "" || s.finished {
			return gateway.Event{Kind: gateway.EventUsage, Usage: usage}, true, nil
		}
		v := finishReason(w.Delta.StopReason)
		reason = &v
		s.finished = true
	case "message_stop":
		return s.messageStopEvent(usage)
	case "content_block_stop":
		return gateway.Event{}, false, nil
	default:
		return gateway.Event{}, false, fmt.Errorf("anthropic: unsupported SSE event %q", w.Type)
	}
	payload, err := json.Marshal(chatChunk{ID: s.id, Object: "chat.completion.chunk", Created: s.created,
		Model: s.model, Choices: []chunkChoice{{Index: 0, Delta: delta, FinishReason: reason}}})
	return gateway.Event{Kind: gateway.EventData, Payload: payload, Usage: usage, TextBytes: textBytes}, true, err
}

// blockStartDelta translates a content_block_start event into a chatDelta.
// ok is false when the event is intentionally skipped (empty text/thinking,
// redacted thinking) rather than an error.
func (s *stream) blockStartDelta(w wireEvent) (chatDelta, int, bool, error) {
	var delta chatDelta
	switch w.ContentBlock.Type {
	case "tool_use":
		if s.tools == nil {
			s.tools = make(map[int]int)
		}
		index := s.toolCount
		s.toolCount++
		s.tools[w.Index] = index
		args := ""
		if len(w.ContentBlock.Input) > 0 && string(w.ContentBlock.Input) != "{}" {
			args = string(w.ContentBlock.Input)
		}
		delta.ToolCalls = []chunkToolCall{{Index: index, ID: w.ContentBlock.ID, Type: "function", Function: callFunction{Name: w.ContentBlock.Name, Arguments: args}}}
		return delta, len(args), true, nil
	case "text":
		if w.ContentBlock.Text == "" {
			return delta, 0, false, nil
		}
		delta.Content = &w.ContentBlock.Text
		return delta, len(w.ContentBlock.Text), true, nil
	case "thinking":
		if w.ContentBlock.Thinking == "" {
			return delta, 0, false, nil
		}
		delta.Reasoning = &w.ContentBlock.Thinking
		return delta, len(w.ContentBlock.Thinking), true, nil
	case "redacted_thinking":
		return delta, 0, false, nil
	default:
		return delta, 0, false, fmt.Errorf("anthropic: unsupported response block %q", w.ContentBlock.Type)
	}
}

// blockDeltaDelta translates a content_block_delta event into a chatDelta.
// ok is false when the event is intentionally skipped (signature_delta).
func (s *stream) blockDeltaDelta(w wireEvent) (chatDelta, int, bool, error) {
	var delta chatDelta
	switch w.Delta.Type {
	case "text_delta":
		delta.Content = &w.Delta.Text
		return delta, len(w.Delta.Text), true, nil
	case "thinking_delta":
		delta.Reasoning = &w.Delta.Thinking
		return delta, len(w.Delta.Thinking), true, nil
	case "input_json_delta":
		index, ok := s.tools[w.Index]
		if !ok {
			return delta, 0, false, fmt.Errorf("anthropic: tool delta before tool start")
		}
		delta.ToolCalls = []chunkToolCall{{Index: index, Function: callFunction{Arguments: w.Delta.PartialJSON}}}
		return delta, len(w.Delta.PartialJSON), true, nil
	case "signature_delta":
		return delta, 0, false, nil
	default:
		return delta, 0, false, fmt.Errorf("anthropic: unsupported delta %q", w.Delta.Type)
	}
}

func (s *stream) messageStopEvent(usage *gateway.Usage) (gateway.Event, bool, error) {
	if !s.finished {
		s.finished = true
		// A missing stop_reason is malformed rather than a successful Chat completion.
		return gateway.Event{}, false, fmt.Errorf("anthropic: message stopped without stop_reason")
	}
	if s.wantUsage && usage != nil {
		payload, err := json.Marshal(chatChunk{ID: s.id, Object: "chat.completion.chunk", Created: s.created,
			Model: s.model, Choices: []chunkChoice{}, Usage: chatUsageFrom(usage)})
		return gateway.Event{Kind: gateway.EventData, Payload: payload, Usage: usage}, true, err
	}
	return gateway.Event{}, false, nil
}
