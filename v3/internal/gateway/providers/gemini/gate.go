package gemini

import (
	"bytes"
	"errors"
	"io"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const maxLifecycleBytes = 1 << 20
const maxLifecycleEvents = 256

// gatedStream buffers lifecycle frames until genuine generated content arrives.
// Only the bounded preamble is copied; reader-backed payloads after opening are
// forwarded directly, and buffered frames retain their original wire order.
type gatedStream struct {
	source   *responseStream
	pending  []gateway.Event
	position int
	bytes    int
	opened   bool
	end      bool
	endEvent gateway.EventKind
	usage    *gateway.Usage
}

func (s *gatedStream) Next() (gateway.Event, error) {
	if s.opened {
		if s.position < len(s.pending) {
			return s.flush(), nil
		}
		return s.source.Next()
	}
	if s.end {
		if s.endEvent == gateway.EventDone {
			s.endEvent = 0
			return gateway.Event{Kind: gateway.EventDone}, nil
		}
		return gateway.Event{}, io.EOF
	}
	for {
		ev, err := s.source.Next()
		if err != nil {
			s.pending = nil
			if errors.Is(err, io.EOF) {
				s.end = true
				if s.usage != nil {
					return gateway.Event{Kind: gateway.EventUsage, Usage: s.usage}, nil
				}
			}
			return gateway.Event{}, err
		}
		if ev.Usage != nil {
			s.usage = ev.Usage
		}
		event, cont := s.handleSourceEvent(ev)
		if cont {
			continue
		}
		return event, nil
	}
}

// handleSourceEvent buffers or forwards one event read from the source
// stream. cont is true when Next's loop should read the next source event
// without returning.
func (s *gatedStream) handleSourceEvent(ev gateway.Event) (gateway.Event, bool) {
	switch ev.Kind {
	case gateway.EventData:
		if s.source.semantic {
			s.pending = append(s.pending, ev)
			s.opened = true
			return s.flush(), false
		}
		if len(s.pending) >= maxLifecycleEvents || len(ev.Payload) > maxLifecycleBytes-s.bytes {
			s.pending = nil
			return gateway.Event{Kind: gateway.EventError, Err: &gateway.UpstreamError{
				Status: http.StatusBadGateway, Type: "upstream_error", Code: "invalid_upstream_response",
				Message: "gemini: lifecycle preamble exceeds limit"}}, false
		}
		ev.Payload = bytes.Clone(ev.Payload)
		s.bytes += len(ev.Payload)
		s.pending = append(s.pending, ev)
		return gateway.Event{}, true
	case gateway.EventUsage:
		return ev, false
	case gateway.EventError:
		s.pending = nil
		return ev, false
	case gateway.EventDone:
		s.pending = nil
		s.end = true
		if s.usage != nil {
			s.endEvent = gateway.EventDone
			return gateway.Event{Kind: gateway.EventUsage, Usage: s.usage}, false
		}
		return ev, false
	}
	return gateway.Event{}, true
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

func semanticResponse(response generateResponse, native bool) bool {
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" || (part.FunctionCall != nil && part.FunctionCall.Name != "") {
				return true
			}
			if native && ((part.InlineData != nil && part.InlineData.Data != "") ||
				(part.FileData != nil && part.FileData.URI != "") || (part.FunctionResponse != nil && part.FunctionResponse.Name != "") ||
				len(part.ExecutableCode) > 0 || len(part.CodeResult) > 0) {
				return true
			}
		}
	}
	return false
}
