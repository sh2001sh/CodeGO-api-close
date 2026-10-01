package coze

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
	"github.com/tidwall/gjson"
)

type messageState struct{ text, reasoning string }

type responseStream struct {
	req                                          *gateway.Request
	body                                         io.ReadCloser
	ctx                                          context.Context
	reader                                       *sse.Reader
	messages                                     map[string]messageState
	wantUsage, jsonResponse, finished, delivered bool
	text, reasoning                              strings.Builder
	usage                                        *gateway.Usage
	queue                                        []gateway.Event
}

func (s *responseStream) Close() error { return s.body.Close() }

func (s *responseStream) Next() (gateway.Event, error) {
	for {
		if len(s.queue) > 0 {
			ev := s.queue[0]
			s.queue = s.queue[1:]
			return ev, nil
		}
		if s.finished {
			return gateway.Event{}, io.EOF
		}
		if s.ctx != nil && s.ctx.Err() != nil {
			s.finished = true
			return gateway.Event{}, s.ctx.Err()
		}
		if s.jsonResponse {
			return s.jsonResponseFailure()
		}
		name, root, event, err, ok := s.readEvent()
		if !ok {
			return event, err
		}
		event, err, cont := s.dispatchEvent(name, root)
		if cont {
			continue
		}
		return event, err
	}
}

// jsonResponseFailure reads the (non-SSE) JSON body Coze returned instead of
// a native event stream and translates it into a terminal failure event.
func (s *responseStream) jsonResponseFailure() (gateway.Event, error) {
	s.finished = true
	body, err := io.ReadAll(io.LimitReader(s.body, maxResponseSize+1))
	if err != nil {
		return gateway.Event{}, err
	}
	if len(body) > maxResponseSize {
		return gateway.Event{}, errors.New("coze: response exceeds size limit")
	}
	root := gjson.ParseBytes(body)
	if !gjson.ValidBytes(body) || !root.IsObject() {
		return s.failure("invalid_response", "Coze returned invalid JSON"), nil
	}
	if code := root.Get("code"); code.Type == gjson.Number && code.Int() != 0 {
		return s.failure(strconv.FormatInt(code.Int(), 10), nativeErrorMessage(root)), nil
	}
	return s.failure("invalid_response", "Coze did not return the requested native event stream"), nil
}

// readEvent pulls and parses the next SSE event. ok is false when Next
// should return (event, err) immediately (terminal condition reached);
// ok is true when name/root are ready for dispatchEvent.
func (s *responseStream) readEvent() (name string, root gjson.Result, event gateway.Event, err error, ok bool) {
	ev, readErr := s.reader.Next()
	if readErr != nil {
		s.finished = true
		if errors.Is(readErr, io.EOF) {
			readErr = io.ErrUnexpectedEOF
		}
		return "", gjson.Result{}, gateway.Event{}, readErr, false // A clean native chat.completed is mandatory.
	}
	name = string(ev.Name)
	if name == "done" {
		s.finished = true
		return "", gjson.Result{}, gateway.Event{}, io.ErrUnexpectedEOF, false
	}
	root = gjson.ParseBytes(ev.Data)
	if !gjson.ValidBytes(ev.Data) || !root.IsObject() {
		s.finished = true
		return "", gjson.Result{}, s.failure("invalid_response", "Coze returned malformed event data"), nil, false
	}
	return name, root, gateway.Event{}, nil, true
}

// dispatchEvent handles one parsed SSE event by name. cont is true when
// Next's loop should continue reading the next event without returning.
func (s *responseStream) dispatchEvent(name string, root gjson.Result) (event gateway.Event, err error, cont bool) {
	switch name {
	case "conversation.message.delta", "conversation.message.completed":
		return s.handleMessageEvent(name, root)
	case "conversation.chat.completed":
		return s.handleChatCompleted(root)
	case "error", "conversation.chat.failed", "conversation.chat.canceled", "conversation.chat.requires_action":
		return s.handleChatFailed(name, root), nil, false
	case "conversation.chat.created", "conversation.chat.in_progress":
		return gateway.Event{}, nil, true // Lifecycle prefixes must not commit the gateway failover gate.
	default:
		s.finished = true
		return s.failure("invalid_response", "Coze returned an unsupported event "+name), nil, false
	}
}

func (s *responseStream) handleMessageEvent(name string, root gjson.Result) (gateway.Event, error, bool) {
	if root.Get("type").Str != "answer" {
		return gateway.Event{}, nil, true // Bot tool and follow-up events are internal.
	}
	text, reasoning, err := s.message(root, name == "conversation.message.completed")
	if err != nil {
		s.finished = true
		return s.failure("invalid_response", err.Error()), nil, false
	}
	if text == "" && reasoning == "" {
		return gateway.Event{}, nil, true
	}
	if !s.req.Stream {
		// Keep partial output observable to accounting without exposing an
		// incomplete JSON response or committing the failover gate.
		return gateway.Event{Kind: gateway.EventUsage, TextBytes: len(text) + len(reasoning)}, nil, false
	}
	delta := map[string]any{}
	if !s.delivered {
		delta["role"] = "assistant"
	}
	if text != "" {
		delta["content"] = text
	}
	if reasoning != "" {
		delta["reasoning_content"] = reasoning
	}
	s.delivered = true
	return gateway.Event{Kind: gateway.EventData, Payload: s.chunk(delta, nil, nil), TextBytes: len(text) + len(reasoning)}, nil, false
}

func (s *responseStream) handleChatCompleted(root gjson.Result) (gateway.Event, error, bool) {
	s.finished = true
	if status := root.Get("status").Str; status != "" && status != "completed" {
		return s.failure("invalid_response", "Coze completion event has an incomplete status"), nil, false
	}
	if code := root.Get("last_error.code"); code.Type == gjson.Number && code.Int() != 0 {
		return s.failure(strconv.FormatInt(code.Int(), 10), nativeErrorMessage(root)), nil, false
	}
	usage, err := nativeUsage(root.Get("usage"))
	if err != nil {
		return s.failure("invalid_response", err.Error()), nil, false
	}
	s.usage = usage
	if s.text.Len()+s.reasoning.Len() == 0 {
		return s.failure("empty_response", "Coze returned no answer"), nil, false
	}
	if !s.req.Stream {
		return s.collectedChatCompletion(usage), nil, false
	}
	if usage != nil {
		ue := gateway.Event{Kind: gateway.EventUsage, Usage: usage}
		if s.wantUsage {
			ue.Kind = gateway.EventData
			ue.Payload = s.chunk(nil, nil, usage)
		}
		s.queue = append(s.queue, ue)
	}
	s.queue = append(s.queue, gateway.Event{Kind: gateway.EventDone})
	return gateway.Event{Kind: gateway.EventData, Payload: s.chunk(map[string]any{}, "stop", nil)}, nil, false
}

func (s *responseStream) collectedChatCompletion(usage *gateway.Usage) gateway.Event {
	message := map[string]any{"role": "assistant", "content": s.text.String()}
	if s.reasoning.Len() > 0 {
		message["reasoning_content"] = s.reasoning.String()
	}
	out := s.envelope("chat.completion")
	out["choices"] = []any{map[string]any{"index": 0, "message": message, "finish_reason": "stop"}}
	if usage != nil {
		out["usage"] = usageJSON(usage)
	}
	payload, _ := json.Marshal(out)
	s.queue = append(s.queue, gateway.Event{Kind: gateway.EventDone})
	return gateway.Event{Kind: gateway.EventData, Payload: payload, Usage: usage}
}

func (s *responseStream) handleChatFailed(name string, root gjson.Result) gateway.Event {
	s.finished = true
	usage, err := nativeUsage(root.Get("usage"))
	if err != nil {
		return s.failure("invalid_response", nativeErrorMessage(root)+": "+err.Error())
	}
	if usage != nil {
		s.usage = usage
	}
	code := "coze_error"
	codeValue := root.Get("code")
	if !codeValue.Exists() {
		codeValue = root.Get("last_error.code")
	}
	if codeValue.Type == gjson.Number && codeValue.Int() != 0 {
		code = strconv.FormatInt(codeValue.Int(), 10)
	}
	if name != "error" && code == "coze_error" {
		code = strings.TrimPrefix(name, "conversation.chat.")
	}
	return s.failure(code, nativeErrorMessage(root))
}

func (s *responseStream) message(root gjson.Result, completed bool) (string, string, error) {
	if role := root.Get("role").Str; role != "" && role != "assistant" {
		return "", "", errors.New("coze: answer role must be assistant")
	}
	if kind := root.Get("content_type").Str; kind != "" && kind != "text" {
		return "", "", errors.New("coze: returned an unsupported answer content type")
	}
	content, reasoning := root.Get("content"), root.Get("reasoning_content")
	if content.Exists() && content.Type != gjson.String {
		return "", "", errors.New("coze: answer content must be text")
	}
	if reasoning.Exists() && reasoning.Type != gjson.String {
		return "", "", errors.New("coze: reasoning content must be text")
	}
	id := root.Get("id").Str
	if id == "" {
		id = "answer"
	}
	if len(id) > 512 {
		return "", "", errors.New("coze: message identifier exceeds size limit")
	}
	if _, exists := s.messages[id]; !exists && len(s.messages) >= 1024 {
		return "", "", errors.New("coze: response has too many answer messages")
	}
	state := s.messages[id]
	text, thought := content.Str, reasoning.Str
	if completed {
		if content.Exists() {
			if !strings.HasPrefix(text, state.text) {
				return "", "", errors.New("coze: final answer disagrees with its deltas")
			}
			text = strings.TrimPrefix(text, state.text)
		}
		if reasoning.Exists() {
			if !strings.HasPrefix(thought, state.reasoning) {
				return "", "", errors.New("coze: final reasoning disagrees with its deltas")
			}
			thought = strings.TrimPrefix(thought, state.reasoning)
		}
	}
	if s.text.Len()+s.reasoning.Len()+len(text)+len(thought) > maxResponseSize {
		return "", "", errors.New("coze: answer exceeds size limit")
	}
	state.text += text
	state.reasoning += thought
	s.messages[id] = state
	s.text.WriteString(text)
	s.reasoning.WriteString(thought)
	return text, thought, nil
}
