package responses

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

const maxLifecycleBytes = 1 << 20
const maxLifecycleEvents = 256

type stream struct {
	body        io.ReadCloser
	reader      *sse.Reader
	buffer      []gateway.Event
	bufferBytes int
	queue       []gateway.Event
	semantic    bool
	terminal    bool
	done        bool
	sawEvent    bool
	tools       toolMeter
	serviceTier string
}

func (s *stream) Next() (gateway.Event, error) {
	for {
		if len(s.queue) > 0 {
			ev := s.queue[0]
			s.queue[0] = gateway.Event{}
			s.queue = s.queue[1:]
			return ev, nil
		}
		if s.terminal {
			if s.done {
				return gateway.Event{}, io.EOF
			}
			s.done = true
			return gateway.Event{Kind: gateway.EventDone}, nil
		}
		wire, err := s.reader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) && s.sawEvent {
				err = io.ErrUnexpectedEOF
			}
			return gateway.Event{}, err
		}
		if len(wire.Data) == 0 {
			continue
		}
		s.sawEvent = true
		ev, terminal, semantic, done, err := s.parseFrame(wire)
		if done {
			return ev, err
		}
		if !s.semantic && !semantic && !terminal {
			if len(s.buffer) >= maxLifecycleEvents || s.bufferBytes+len(wire.Data) > maxLifecycleBytes {
				return gateway.Event{}, sse.ErrEventTooLarge
			}
			ev.Payload = bytes.Clone(ev.Payload) // reader storage is reused while the gate is closed
			s.buffer = append(s.buffer, ev)
			s.bufferBytes += len(ev.Payload)
			continue
		}
		s.semantic = true
		s.terminal = terminal
		if len(s.buffer) > 0 {
			// No additional upstream read occurs until this queue is consumed.
			s.queue = append(s.buffer, ev)
			s.buffer = nil
			continue
		}
		return ev, nil
	}
}

// parseFrame decodes a single SSE frame into its gateway event along with
// the lifecycle flags Next needs to drive buffering. done reports that ev
// (and err) must be returned immediately rather than go through buffering.
func (s *stream) parseFrame(wire sse.Event) (ev gateway.Event, terminal, semantic, done bool, err error) {
	// A data-only Chat terminator cannot substitute for response.completed.
	if bytes.Equal(wire.Data, []byte("[DONE]")) {
		return gateway.Event{}, false, false, true, io.ErrUnexpectedEOF
	}
	root, failure := parseBody(wire.Data)
	if failure != nil {
		return gateway.Event{Kind: gateway.EventError, Err: failure}, false, false, true, nil
	}
	name := root.Get("type").Str
	if name == "" {
		name = string(wire.Name)
	}
	ev = gateway.Event{Kind: gateway.EventData, Name: name, Payload: wire.Data}
	response := root.Get("response")
	if tier := response.Get("service_tier").Str; tier != "" {
		s.serviceTier = tier
	}
	ev.ServiceTier = s.serviceTier
	ev.Usage = s.tools.apply(root, parseUsage(response.Get("usage")))
	if ev.Usage != nil {
		ev.Usage.ServiceTier = s.serviceTier
	}
	if failure = responseError(response); failure == nil {
		failure = responseError(root)
	}
	if name == "response.failed" || name == "error" {
		if failure == nil {
			failure = responseError(gjson.Parse(`{"status":"failed"}`))
		}
	}
	if failure != nil {
		ev.Kind, ev.Err = gateway.EventError, failure
		s.buffer = nil
		return ev, false, false, true, nil
	}
	textBytes, semantic := deltaContent(root, name)
	ev.TextBytes = textBytes
	terminal = name == "response.completed" || name == "response.incomplete"
	if terminal && !s.semantic && !semantic && !hasOutput(response.Get("output")) {
		s.buffer = nil
		return gateway.Event{Kind: gateway.EventError, Err: emptyError(), Usage: ev.Usage, ServiceTier: ev.ServiceTier}, false, false, true, nil
	}
	if terminal && !s.semantic {
		ev.TextBytes = outputTextBytes(response.Get("output"))
	}
	return ev, terminal, semantic, false, nil
}

func deltaContent(root gjson.Result, name string) (int, bool) {
	if strings.HasSuffix(name, ".delta") {
		if delta := root.Get("delta").Str; delta != "" {
			return len(delta), true
		}
	}
	if name == "response.output_item.added" || name == "response.output_item.done" {
		item := root.Get("item")
		return 0, semanticItem(item)
	}
	if name == "response.image_generation_call.partial_image" {
		return 0, root.Get("partial_image_b64").Str != ""
	}
	return 0, false
}

func (s *stream) Close() error { return s.body.Close() }
