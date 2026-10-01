package bridge

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type chatDelta struct {
	Role             string         `json:"role"`
	Content          string         `json:"content"`
	ReasoningContent string         `json:"reasoning_content"`
	Reasoning        string         `json:"reasoning"`
	Refusal          string         `json:"refusal"`
	ToolCalls        []chatToolCall `json:"tool_calls"`
}
type chatChoice struct {
	Index        int        `json:"index"`
	Delta        *chatDelta `json:"delta"`
	Message      *chatDelta `json:"message"`
	FinishReason string     `json:"finish_reason"`
}
type chatFrame struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
}

type stream struct {
	protocol     gateway.Protocol
	streaming    bool
	source       gateway.EventStream
	queue        []gateway.Event
	usage        *gateway.Usage
	done         bool
	started      bool
	finishReason string
	responses    *ResponsesEmitter
	anthropic    anthropicEmitter
	gemini       geminiEmitter
	collected    collectedResponse
}

func newStream(req *gateway.Request, source gateway.EventStream) *stream {
	return &stream{protocol: req.Protocol, streaming: req.Stream, source: source,
		responses: NewResponsesEmitter("resp_"+req.ID, req.Model),
		anthropic: anthropicEmitter{id: "msg_" + req.ID, model: req.Model}, gemini: geminiEmitter{model: req.Model}}
}

func (s *stream) Close() error { return s.source.Close() }

func (s *stream) Next() (gateway.Event, error) {
	for len(s.queue) == 0 {
		if s.done {
			return gateway.Event{}, io.EOF
		}
		event, err := s.source.Next()
		if err != nil {
			if err != io.EOF {
				return gateway.Event{}, err
			}
			if ev, herr, ret := s.handleEnd(gateway.Event{}, io.EOF); ret {
				return ev, herr
			}
			continue
		}
		if event.Usage != nil {
			s.usage = event.Usage
		}
		switch event.Kind {
		case gateway.EventError:
			s.done = true
			if event.Usage == nil {
				event.Usage = s.usage
			}
			return event, nil
		case gateway.EventUsage:
			return gateway.Event{Kind: gateway.EventUsage, Usage: s.usage, TextBytes: event.TextBytes}, nil
		case gateway.EventDone:
			if ev, herr, ret := s.handleEnd(gateway.Event{Kind: gateway.EventDone}, nil); ret {
				return ev, herr
			}
		case gateway.EventData:
			if err = s.convert(event); err != nil {
				return gateway.Event{}, err
			}
		}
	}
	event := s.queue[0]
	s.queue = s.queue[1:]
	return event, nil
}

// handleEnd marks the stream done and runs the collected-response or
// streaming finisher. ret is true when Next should return (event, err)
// immediately; ret is false when Next should continue draining its queue.
func (s *stream) handleEnd(doneEvent gateway.Event, doneErr error) (event gateway.Event, err error, ret bool) {
	s.done = true
	if !s.streaming && s.collected.started {
		if err = s.finishCollected(); err != nil {
			return gateway.Event{}, err, true
		}
		return gateway.Event{}, nil, false
	}
	if s.streaming && s.started {
		if err = s.finish(); err != nil {
			return gateway.Event{}, err, true
		}
		return gateway.Event{}, nil, false
	}
	return doneEvent, doneErr, true
}

func (s *stream) convert(event gateway.Event) error {
	var frame chatFrame
	if err := json.Unmarshal(event.Payload, &frame); err != nil {
		return fmt.Errorf("bridge: invalid Chat response: %w", err)
	}
	if len(frame.Choices) > 1 {
		return fmt.Errorf("bridge: multiple Chat choices cannot be converted to native protocols")
	}
	if len(frame.Choices) == 0 {
		if event.Usage != nil {
			s.queue = append(s.queue, gateway.Event{Kind: gateway.EventUsage, Usage: event.Usage})
		}
		return nil
	}
	choice := frame.Choices[0]
	if choice.Index != 0 {
		return fmt.Errorf("bridge: unsupported Chat choice index %d", choice.Index)
	}
	delta := extractDelta(choice)
	if choice.FinishReason != "" {
		s.finishReason = choice.FinishReason
	}
	if !s.streaming {
		return s.convertCollected(event, choice, delta)
	}
	return s.convertStreaming(event, delta)
}

// extractDelta normalizes a Chat choice's delta/message field into a single
// chatDelta, assigning stable indexes to tool calls carried on a full message.
func extractDelta(choice chatChoice) chatDelta {
	var delta chatDelta
	if choice.Delta != nil {
		delta = *choice.Delta
	} else if choice.Message != nil {
		delta = *choice.Message
		for index := range delta.ToolCalls {
			delta.ToolCalls[index].Index = index
		}
	}
	if delta.ReasoningContent == "" {
		delta.ReasoningContent = delta.Reasoning
	}
	return delta
}

func (s *stream) convertCollected(event gateway.Event, choice chatChoice, delta chatDelta) error {
	if choice.Message == nil && choice.Delta != nil {
		if err := s.collected.add(delta); err != nil {
			return err
		}
		s.queue = append(s.queue, accountEvents(nil, event, delta, s.usage)...)
		return nil
	}
	payload, err := singleResponse(s.protocol, s.responses.id, s.responses.model, delta, s.finishReason, s.usage)
	if err != nil {
		return err
	}
	s.queue = append(s.queue, gateway.Event{Kind: gateway.EventData, Payload: payload, Usage: s.usage, TextBytes: consumedBytes(event, delta)})
	s.done = true
	return nil
}

func (s *stream) convertStreaming(event gateway.Event, delta chatDelta) error {
	if delta.Content == "" && delta.ReasoningContent == "" && delta.Refusal == "" && len(delta.ToolCalls) == 0 {
		s.queue = append(s.queue, accountEvents(nil, event, delta, s.usage)...)
		return nil
	}
	s.started = true
	var events []gateway.Event
	var err error
	switch s.protocol {
	case gateway.ProtocolResponses:
		events, err = s.responses.onDelta(delta)
	case gateway.ProtocolAnthropic:
		events, err = s.anthropic.onDelta(delta)
	case gateway.ProtocolGemini:
		events, err = s.gemini.onDelta(delta)
	}
	if err != nil {
		return err
	}
	events = accountEvents(events, event, delta, s.usage)
	s.queue = append(s.queue, events...)
	return nil
}

func (s *stream) finish() error {
	var events []gateway.Event
	var err error
	switch s.protocol {
	case gateway.ProtocolResponses:
		events, err = s.responses.Finish(s.usage, s.finishReason)
	case gateway.ProtocolAnthropic:
		events, err = s.anthropic.finish(s.usage, s.finishReason)
	case gateway.ProtocolGemini:
		events, err = s.gemini.finish(s.usage, s.finishReason)
	}
	if err != nil {
		return err
	}
	s.queue = append(s.queue, events...)
	s.queue = append(s.queue, gateway.Event{Kind: gateway.EventDone})
	return nil
}

func encodedEvent(name string, v interface{}, textBytes int) gateway.Event {
	data, _ := json.Marshal(v) // native wire structs only contain JSON-serializable fields
	return gateway.Event{Kind: gateway.EventData, Name: name, Payload: data, TextBytes: textBytes}
}

// Accounting follows upstream consumption rather than native lifecycle frames.
// Buffered tool fragments use a hidden event, retaining their cost on a cut or
// cancellation without fabricating a native data payload or double-counting
// arguments when their completed native tool call is eventually emitted.
func accountEvents(events []gateway.Event, source gateway.Event, delta chatDelta, usage *gateway.Usage) []gateway.Event {
	consumed := consumedBytes(source, delta)
	for index := range events {
		events[index].TextBytes = 0
	}
	if len(events) == 0 {
		if consumed == 0 && source.Usage == nil {
			return nil
		}
		return []gateway.Event{{Kind: gateway.EventUsage, Usage: usage, TextBytes: consumed}}
	}
	last := &events[len(events)-1]
	last.TextBytes = consumed
	last.Usage = source.Usage
	return events
}
func consumedBytes(source gateway.Event, delta chatDelta) int {
	if source.TextBytes > 0 {
		return source.TextBytes
	}
	count := len(delta.Content) + len(delta.ReasoningContent) + len(delta.Refusal)
	for _, tool := range delta.ToolCalls {
		count += len(tool.Function.Arguments)
	}
	return count
}
