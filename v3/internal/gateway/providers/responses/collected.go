package responses

import (
	"io"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// collected exposes a streaming upstream's terminal response as one JSON body
// when the client requested non-streaming output (subscription providers can
// require SSE even for these callers).
type collected struct {
	source gateway.EventStream
	done   bool
}

func (s *collected) Next() (gateway.Event, error) {
	if s.done {
		return gateway.Event{}, io.EOF
	}
	for {
		ev, err := s.source.Next()
		if err != nil {
			s.done = true
			return gateway.Event{}, err
		}
		if ev.Kind == gateway.EventError {
			s.done = true
			ev.Name, ev.Payload = "", nil
			return ev, nil
		}
		if ev.Kind == gateway.EventDone {
			s.done = true
			return gateway.Event{}, io.ErrUnexpectedEOF
		}
		if ev.Name != "response.completed" && ev.Name != "response.incomplete" {
			continue
		}
		s.done = true
		root, failure := parseBody(ev.Payload)
		if failure != nil {
			return gateway.Event{Kind: gateway.EventError, Err: failure}, nil
		}
		response := root.Get("response")
		if !response.IsObject() {
			return gateway.Event{}, io.ErrUnexpectedEOF
		}
		return gateway.Event{Kind: gateway.EventData, Payload: []byte(response.Raw), Usage: ev.Usage, TextBytes: outputTextBytes(response.Get("output"))}, nil
	}
}

func (s *collected) Close() error { return s.source.Close() }
