package openai

import (
	"bytes"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
	"github.com/tidwall/gjson"
)

// Next holds lifecycle-only chunks behind the first semantic event. The
// bounded copies are necessary because the SSE reader reuses its storage.
func (s *stream) Next() (gateway.Event, error) {
	for {
		if len(s.queue) > 0 {
			ev := s.queue[0]
			s.queue[0] = gateway.Event{}
			s.queue = s.queue[1:]
			return ev, nil
		}
		ev, err := s.nextRaw()
		if err != nil {
			return gateway.Event{}, err
		}
		if ev.Kind == gateway.EventError || ev.Kind == gateway.EventDone {
			s.buffer = nil
			return ev, nil
		}
		if ev.Kind == gateway.EventUsage {
			return ev, nil
		}
		if s.semantic {
			return ev, nil
		}
		if meaningful(ev.Payload) {
			s.semantic = true
			if len(s.buffer) > 0 {
				s.queue = append(s.buffer, ev)
				s.buffer = nil
				continue
			}
			return ev, nil
		}
		if len(s.buffer) >= 256 || s.bufferBytes+len(ev.Payload) > 1<<20 {
			return gateway.Event{}, sse.ErrEventTooLarge
		}
		ev.Payload = bytes.Clone(ev.Payload)
		s.buffer = append(s.buffer, ev)
		s.bufferBytes += len(ev.Payload)
		if ev.Usage != nil || ev.ServiceTier != "" {
			return gateway.Event{Kind: gateway.EventUsage, Usage: ev.Usage, ServiceTier: ev.ServiceTier}, nil
		}
	}
}

func meaningful(data []byte) bool {
	return meaningfulChoices(data, "delta")
}

func meaningfulChoices(data []byte, field string) bool {
	for _, choice := range gjson.GetBytes(data, "choices").Array() {
		delta := choice.Get(field)
		if delta.Get("content").Str != "" || delta.Get("reasoning_content").Str != "" || delta.Get("reasoning").Str != "" || delta.Get("refusal").Str != "" {
			return true
		}
		for _, call := range delta.Get("tool_calls").Array() {
			if call.Get("id").Str != "" || call.Get("function.name").Str != "" || call.Get("function.arguments").Str != "" {
				return true
			}
		}
		if delta.Get("function_call").Exists() || delta.Get("audio").Exists() {
			return true
		}
	}
	return false
}
