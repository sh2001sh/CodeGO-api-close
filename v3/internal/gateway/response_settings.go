package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"sort"
)

// ApplyResponseSettings applies channel presentation settings after native
// decoding. Token accounting remains attached to the original provider event.
func ApplyResponseSettings(source EventStream, req *Request, target Target) EventStream {
	if req.Protocol != ProtocolOpenAIChat {
		return source
	}
	force, _ := target.Settings["force_format"].(bool)
	thinking, _ := target.Settings["thinking_to_content"].(bool)
	thinking = thinking && req.Stream // v2 applies this setting to streams only
	if !force && !thinking {
		return source
	}
	return &responseSettingsStream{source: source, force: force, thinking: thinking,
		streaming: req.Stream, open: make(map[int]bool)}
}

type responseSettingsStream struct {
	source                     EventStream
	force, thinking, streaming bool
	open                       map[int]bool
	last                       map[string]json.RawMessage
	pending                    *Event
	ended                      bool
}

func (s *responseSettingsStream) Close() error { return s.source.Close() }

func (s *responseSettingsStream) Next() (Event, error) {
	if s.pending != nil {
		event := *s.pending
		s.pending = nil
		return event, nil
	}
	if s.ended {
		return Event{}, io.EOF
	}
	event, err := s.source.Next()
	if event.Kind == EventDone || errors.Is(err, io.EOF) {
		closed, closeErr := s.closeThinking()
		if closeErr != nil {
			return Event{}, closeErr
		}
		if err != nil {
			s.ended = true
		} else {
			s.pending = &event
		}
		if closed != nil {
			return *closed, nil
		}
		if err != nil {
			return event, err
		}
		s.pending = nil
		return event, nil
	}
	if err != nil || event.Kind != EventData {
		return event, err
	}
	if s.thinking {
		event.Payload, err = s.convertThinking(event.Payload)
		if err != nil {
			return Event{}, err
		}
	}
	if s.force {
		event.Payload, err = forceResponseFormat(event.Payload, s.streaming)
	}
	return event, err
}

func (s *responseSettingsStream) convertThinking(payload []byte) ([]byte, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, err
	}
	var choices []map[string]json.RawMessage
	if raw := envelope["choices"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &choices); err != nil {
			return nil, err
		}
	}
	s.last = envelope
	for _, choice := range choices {
		if err := s.convertThinkingChoice(choice); err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(choices)
	if err != nil {
		return nil, err
	}
	envelope["choices"] = encoded
	return json.Marshal(envelope)
}

// convertThinkingChoice rewrites one choice's delta in place, wrapping
// reasoning text in <think> markers and tracking per-index open/close state
// on s.open across the stream's events.
func (s *responseSettingsStream) convertThinkingChoice(choice map[string]json.RawMessage) error {
	var index int
	if raw := choice["index"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &index); err != nil {
			return err
		}
	}
	var delta map[string]json.RawMessage
	if raw := choice["delta"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &delta); err != nil {
			return err
		}
	}
	if delta == nil {
		delta = make(map[string]json.RawMessage)
	}
	reasoning := responseSettingText(delta["reasoning_content"])
	if reasoning == "" && (len(delta["reasoning_content"]) == 0 || string(delta["reasoning_content"]) == "null") {
		reasoning = responseSettingText(delta["reasoning"])
	}
	content := responseSettingText(delta["content"])
	text := ""
	if reasoning != "" {
		if !s.open[index] {
			text = "<think>\n"
			s.open[index] = true
		}
		text += reasoning
	}
	finished := responseSettingText(choice["finish_reason"]) != ""
	if s.open[index] && (content != "" || finished) {
		text += "\n</think>\n"
		delete(s.open, index)
	}
	if text != "" {
		text += content
		encoded, err := json.Marshal(text)
		if err != nil {
			return err
		}
		delta["content"] = encoded
	}
	delete(delta, "reasoning_content")
	delete(delta, "reasoning")
	encoded, err := json.Marshal(delta)
	if err != nil {
		return err
	}
	choice["delta"] = encoded
	return nil
}

func (s *responseSettingsStream) closeThinking() (*Event, error) {
	if len(s.open) == 0 {
		return nil, nil
	}
	indices := make([]int, 0, len(s.open))
	for index := range s.open {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	choices := make([]map[string]any, 0, len(indices))
	for _, index := range indices {
		choices = append(choices, map[string]any{"index": index,
			"delta": map[string]any{"content": "\n</think>\n"}, "finish_reason": nil})
	}
	encoded, err := json.Marshal(choices)
	if err != nil {
		return nil, err
	}
	// The generated closing marker carries no usage or generated text bytes.
	envelope := make(map[string]json.RawMessage, len(s.last))
	for key, value := range s.last {
		if key != "usage" {
			envelope[key] = value
		}
	}
	envelope["choices"] = encoded
	payload, err := json.Marshal(envelope)
	if err == nil && s.force {
		payload, err = forceResponseFormat(payload, true)
	}
	if err != nil {
		return nil, err
	}
	clear(s.open)
	return &Event{Kind: EventData, Payload: payload}, nil
}

func responseSettingText(raw json.RawMessage) string {
	var text string
	_ = json.Unmarshal(raw, &text)
	return text
}
