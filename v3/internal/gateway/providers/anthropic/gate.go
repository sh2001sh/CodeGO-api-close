package anthropic

import (
	"fmt"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const maxLifecycleBytes = 1 << 20
const maxLifecycleEvents = 256

// gatedStream keeps lifecycle frames private until the first semantic output.
// Native SSE payloads borrow the reader buffer, so only the bounded preamble
// needs copying. After the gate opens, normal events remain zero-copy.
type gatedStream struct {
	source   *stream
	pending  []gateway.Event
	position int
	bytes    int
	opened   bool
	terminal bool
	usage    *gateway.Usage
}

func (s *gatedStream) Next() (gateway.Event, error) {
	if s.position < len(s.pending) && s.opened {
		return s.flush(), nil
	}
	if s.terminal {
		s.terminal = false
		return gateway.Event{Kind: gateway.EventDone}, nil
	}
	if s.opened {
		return s.source.Next()
	}
	for {
		ev, err := s.source.Next()
		if err != nil {
			s.pending = nil
			return gateway.Event{}, err
		}
		if ev.Usage != nil {
			s.usage = ev.Usage
		}
		switch ev.Kind {
		case gateway.EventData:
			if s.source.semantic {
				s.pending = append(s.pending, ev)
				s.opened = true
				return s.flush(), nil
			}
			if len(s.pending) >= maxLifecycleEvents || len(ev.Payload) > maxLifecycleBytes-s.bytes {
				s.pending = nil
				return decodeError(fmt.Errorf("anthropic: lifecycle preamble exceeds limit")), nil
			}
			ev.Payload = append([]byte(nil), ev.Payload...)
			s.bytes += len(ev.Payload)
			s.pending = append(s.pending, ev)
		case gateway.EventUsage:
			return ev, nil
		case gateway.EventError:
			s.pending = nil
			return ev, nil
		case gateway.EventDone:
			s.pending = nil
			if s.usage != nil {
				s.terminal = true
				return gateway.Event{Kind: gateway.EventUsage, Usage: s.usage}, nil
			}
			return ev, nil
		}
	}
}

func (s *gatedStream) flush() gateway.Event {
	ev := s.pending[s.position]
	s.pending[s.position] = gateway.Event{}
	s.position++
	if s.position == len(s.pending) {
		s.pending = nil
		s.position = 0
	}
	return ev
}

func (s *gatedStream) Close() error { return s.source.Close() }

func semanticOutput(w wireEvent, chat bool) bool {
	switch w.Type {
	case "content_block_start":
		switch w.ContentBlock.Type {
		case "text":
			return w.ContentBlock.Text != ""
		case "thinking":
			return w.ContentBlock.Thinking != ""
		case "redacted_thinking":
			return !chat
		default:
			return w.ContentBlock.Type != ""
		}
	case "content_block_delta":
		return w.Delta.Text != "" || w.Delta.Thinking != "" || w.Delta.PartialJSON != ""
	default:
		return false
	}
}
