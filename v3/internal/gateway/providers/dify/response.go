package dify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
	"github.com/tidwall/gjson"
)

type responseStream struct {
	body                                io.ReadCloser
	reader                              *sse.Reader
	stream, wantUsage, delivered, ended bool
	id, model                           string
	conversationID                      string
	created                             int64
	pending                             []gateway.Event
}

func (s *responseStream) Close() error { return s.body.Close() }

func (s *responseStream) Next() (gateway.Event, error) {
	for {
		if len(s.pending) > 0 {
			ev := s.pending[0]
			s.pending[0] = gateway.Event{}
			s.pending = s.pending[1:]
			return ev, nil
		}
		if s.ended {
			return gateway.Event{}, io.EOF
		}
		data, name, err := s.read()
		if err != nil {
			s.ended = true
			return gateway.Event{}, err
		}
		root := gjson.ParseBytes(data)
		if !gjson.ValidBytes(data) || !root.IsObject() {
			return s.failure("invalid_response", "Dify returned invalid JSON"), nil
		}
		if event := root.Get("event"); event.Exists() {
			if event.Type != gjson.String {
				return s.failure("invalid_response", "Dify event must be a string"), nil
			}
			name = event.Str
		}
		if name == "error" || root.Get("code").Exists() && root.Get("message").Exists() && !root.Get("answer").Exists() {
			code, message := root.Get("code").String(), root.Get("message").Str
			if code == "" {
				code = "dify_error"
			}
			if message == "" {
				message = "Dify reported an error"
			}
			return s.failure(code, message), nil
		}
		s.captureEnvelopeIDs(root)
		if !s.stream {
			return s.blockingResult(root, name)
		}
		event, err, cont := s.handleStreamEvent(name, root)
		if cont {
			continue
		}
		return event, err
	}
}

// captureEnvelopeIDs records message/conversation identifiers and the
// created timestamp the first time they appear on an event envelope.
func (s *responseStream) captureEnvelopeIDs(root gjson.Result) {
	if id := root.Get("message_id").Str; id != "" && !s.delivered {
		s.id = id
	}
	if id := root.Get("id").Str; id != "" && !s.delivered {
		s.id = id
	}
	if id := root.Get("conversation_id").Str; id != "" {
		s.conversationID = id
	}
	if s.created <= 0 {
		if timestamp := root.Get("created_at").Int(); timestamp > 0 {
			s.created = timestamp
		}
	}
}

// blockingResult finishes a non-streaming request from its single response
// envelope.
func (s *responseStream) blockingResult(root gjson.Result, name string) (gateway.Event, error) {
	s.ended = true
	if name != "" && name != "message" && name != "agent_message" {
		return s.failure("invalid_response", "Dify blocking response is incomplete"), nil
	}
	answer := root.Get("answer")
	if answer.Type != gjson.String {
		return s.failure("invalid_response", "Dify blocking response has no answer"), nil
	}
	usage, err := parseUsage(root.Get("metadata.usage"))
	if err != nil {
		return s.failure("invalid_response", err.Error()), nil
	}
	if answer.Str == "" {
		ev := s.failure("empty_response", "Dify returned no output")
		ev.Usage = usage
		return ev, nil
	}
	return s.single(answer.Str, root.Get("conversation_id").Str, usage)
}

// handleStreamEvent dispatches one named streaming event. cont is true when
// Next's loop should read the next event without returning.
func (s *responseStream) handleStreamEvent(name string, root gjson.Result) (gateway.Event, error, bool) {
	switch name {
	case "message", "agent_message":
		return s.handleMessageEvent(root)
	case "message_end":
		event, err := s.handleMessageEnd(root)
		return event, err, false
	case "ping", "agent_thought", "message_file", "workflow_started", "node_started", "node_finished", "node_retry", "iteration_started", "iteration_next", "iteration_completed", "loop_started", "loop_next", "loop_completed", "text_chunk", "tts_message", "tts_message_end":
		// These are application lifecycle/annotation events, not Chat answer deltas.
		return gateway.Event{}, nil, true
	case "workflow_finished":
		if status := root.Get("data.status").Str; status != "" && status != "succeeded" {
			return s.failure("dify_workflow_failed", "Dify workflow did not succeed"), nil, false
		}
		// Workflow completion is not the Chat message_end terminal.
		return gateway.Event{}, nil, true
	case "message_replace", "text_replace":
		return s.failure("unsupported_response", "Dify message replacement cannot be represented as Chat deltas"), nil, false
	default:
		return s.failure("invalid_response", "Dify returned an unsupported stream event"), nil, false
	}
}

func (s *responseStream) handleMessageEvent(root gjson.Result) (gateway.Event, error, bool) {
	answer := root.Get("answer")
	if answer.Type != gjson.String {
		return s.failure("invalid_response", "Dify message has no text answer"), nil, false
	}
	if answer.Str == "" {
		return gateway.Event{}, nil, true
	}
	text := answer.Str
	// Preserve v2's text markers for Dify application reasoning output.
	if text == "<details style=\"color:gray;background-color: #f8f8f8;padding: 8px;border-radius: 4px;\" open> <summary> Thinking... </summary>\n" {
		text = "<think>"
	}
	if text == "</details>" {
		text = "</think>"
	}
	event, err := s.chunk(text, nil, nil, false)
	return event, err, false
}

func (s *responseStream) handleMessageEnd(root gjson.Result) (gateway.Event, error) {
	s.ended = true
	usage, err := parseUsage(root.Get("metadata.usage"))
	if err != nil {
		return s.failure("invalid_response", err.Error()), nil
	}
	if !s.delivered {
		ev := s.failure("empty_response", "Dify returned no output")
		ev.Usage = usage
		return ev, nil
	}
	reason := "stop"
	finish, err := s.chunk("", &reason, nil, false)
	if err != nil {
		return gateway.Event{}, err
	}
	if usage != nil {
		ev := gateway.Event{Kind: gateway.EventUsage, Usage: usage}
		if s.wantUsage {
			ev, err = s.chunk("", nil, usage, true)
			if err != nil {
				return gateway.Event{}, err
			}
		}
		s.pending = append(s.pending, ev)
	}
	s.pending = append(s.pending, gateway.Event{Kind: gateway.EventDone})
	return finish, nil
}

func (s *responseStream) read() ([]byte, string, error) {
	if !s.stream {
		data, err := io.ReadAll(io.LimitReader(s.body, maxResponseSize+1))
		if err == nil && len(data) > maxResponseSize {
			err = sse.ErrEventTooLarge
		}
		return data, "", err
	}
	for {
		ev, err := s.reader.Next()
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		if err != nil {
			return nil, "", err
		}
		if len(ev.Data) == 0 {
			if string(ev.Name) == "ping" || len(ev.Name) == 0 {
				continue
			}
			return nil, "", fmt.Errorf("dify: empty stream event")
		}
		return ev.Data, string(ev.Name), nil
	}
}

func (s *responseStream) failure(code, message string) gateway.Event {
	s.ended = true
	return gateway.Event{Kind: gateway.EventError, Err: &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: code, Message: message}}
}

func (s *responseStream) chunk(text string, finish *string, usage *gateway.Usage, usageOnly bool) (gateway.Event, error) {
	delta := map[string]any{}
	if !s.delivered && !usageOnly {
		delta["role"] = "assistant"
	}
	if text != "" {
		delta["content"] = text
		s.delivered = true
	}
	choices := []any{}
	if !usageOnly {
		choices = append(choices, map[string]any{"index": 0, "delta": delta, "finish_reason": finish})
	}
	out := map[string]any{"id": s.id, "object": "chat.completion.chunk", "created": s.created, "model": s.model, "choices": choices}
	if s.conversationID != "" {
		out["conversation_id"] = s.conversationID
	}
	if usage != nil {
		out["usage"] = usageJSON(usage)
	}
	payload, err := json.Marshal(out)
	return gateway.Event{Kind: gateway.EventData, Payload: payload, TextBytes: len(text), Usage: usage}, err
}

func (s *responseStream) single(text, conversationID string, usage *gateway.Usage) (gateway.Event, error) {
	out := map[string]any{"id": s.id, "object": "chat.completion", "created": s.created, "model": s.model,
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}}}
	if conversationID != "" {
		out["conversation_id"] = conversationID
	}
	if usage != nil {
		out["usage"] = usageJSON(usage)
	}
	payload, err := json.Marshal(out)
	return gateway.Event{Kind: gateway.EventData, Payload: payload, TextBytes: len(text), Usage: usage}, err
}
