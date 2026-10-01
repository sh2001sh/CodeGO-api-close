package tencent

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
	"github.com/tidwall/gjson"
)

type responseStream struct {
	body                               io.ReadCloser
	reader                             *sse.Reader
	stream, wantUsage, semantic, ended bool
	requestedStream                    bool
	id, model                          string
	created                            int64
	seen, finished                     bool
}

func newResponseStream(req *gateway.Request, resp *http.Response) *responseStream {
	s := &responseStream{body: resp.Body, stream: req.Stream && strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream"), requestedStream: req.Stream,
		wantUsage: gjson.GetBytes(req.Body, "stream_options.include_usage").Bool(), id: "chatcmpl-" + req.ID, model: req.Model,
		created: req.Received.Unix()}
	if s.stream {
		s.reader = sse.NewReader(resp.Body, 0)
	}
	return s
}

func (s *responseStream) Next() (gateway.Event, error) {
	for {
		if s.ended {
			return gateway.Event{}, io.EOF
		}
		data, err := s.read()
		if err != nil {
			s.ended = true
			if s.stream && errors.Is(err, io.EOF) {
				return s.done()
			}
			return gateway.Event{}, err
		}
		if s.stream && bytes.Equal(data, []byte("[DONE]")) {
			s.ended = true
			return s.done()
		}
		native, err := decodeNative(data)
		if err != nil {
			s.ended = true
			return gateway.Event{}, err
		}
		usage, err := parseUsage(native.Usage)
		if err != nil {
			s.ended = true
			return gateway.Event{}, err
		}
		if failure := responseError(native.Error); failure != nil {
			s.ended = true
			return gateway.Event{Kind: gateway.EventError, Err: failure, Usage: usage}, nil
		}
		if s.requestedStream && !s.stream {
			s.ended = true
			return gateway.Event{Kind: gateway.EventError, Err: upstream("invalid_response", "Tencent returned JSON instead of a stream"), Usage: usage}, nil
		}
		if native.ID != "" {
			s.id = native.ID
		}
		if native.Created > 0 {
			s.created = native.Created
		}
		if !s.stream {
			s.ended = true
		}
		event, err := s.convert(native, usage)
		if err != nil || event.Kind != 0 {
			return event, err
		}
	}
}

func (s *responseStream) read() ([]byte, error) {
	if s.stream {
		for {
			ev, err := s.reader.Next()
			if err != nil {
				return nil, err
			}
			if len(ev.Data) > 0 {
				return ev.Data, nil
			}
		}
	}
	data, err := io.ReadAll(io.LimitReader(s.body, maxJSONBody+1))
	if err == nil && len(data) > maxJSONBody {
		err = sse.ErrEventTooLarge
	}
	return data, err
}

func decodeNative(data []byte) (nativeResponse, error) {
	var native nativeResponse
	if !gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject() {
		return native, errors.New("tencent: invalid response JSON")
	}
	if err := json.Unmarshal(data, &native); err != nil {
		return native, errors.New("tencent: invalid response fields")
	}
	if len(native.Response) > 0 {
		envelope := native.Response
		native = nativeResponse{}
		if !gjson.ParseBytes(envelope).IsObject() || json.Unmarshal(envelope, &native) != nil {
			return native, errors.New("tencent: invalid response envelope")
		}
	}
	return native, nil
}

func (s *responseStream) done() (gateway.Event, error) {
	if !s.seen || !s.finished {
		return gateway.Event{}, io.ErrUnexpectedEOF
	}
	if !s.semantic {
		return gateway.Event{Kind: gateway.EventError, Err: upstream("empty_response", "Tencent returned no output")}, nil
	}
	return gateway.Event{Kind: gateway.EventDone}, nil
}

func (s *responseStream) convert(native nativeResponse, usage *gateway.Usage) (gateway.Event, error) {
	out := chatResponse{ID: s.id, Model: s.model, Created: s.created, Object: "chat.completion", Choices: []chatChoice{}}
	if s.stream {
		out.Object = "chat.completion.chunk"
	}
	if len(native.Choices) > 1 {
		return gateway.Event{}, errors.New("tencent: unexpected multiple choices")
	}
	textBytes, err := s.convertChoices(native.Choices, &out)
	if err != nil {
		return gateway.Event{}, err
	}
	if textBytes > 0 {
		s.semantic = true
	}
	if !s.semantic {
		return s.emptyOutputEvent(usage)
	}
	if !s.stream && len(out.Choices) == 0 {
		return gateway.Event{Kind: gateway.EventError, Err: upstream("empty_response", "Tencent returned no choices"), Usage: usage}, nil
	}
	if usage != nil && (!s.stream || s.wantUsage) {
		out.Usage = &chatUsage{PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, TotalTokens: usage.PromptTokens + usage.CompletionTokens}
	}
	if len(out.Choices) == 0 {
		if usage == nil {
			return gateway.Event{}, errors.New("tencent: response has no choices or usage")
		}
		if !s.wantUsage {
			return gateway.Event{Kind: gateway.EventUsage, Usage: usage}, nil
		}
	}
	payload, err := json.Marshal(out)
	return gateway.Event{Kind: gateway.EventData, Payload: payload, Usage: usage, TextBytes: textBytes}, err
}

// convertChoices converts Tencent's native choices into out.Choices,
// tracking per-stream state (seen/finished) needed by later frames.
func (s *responseStream) convertChoices(choices []nativeChoice, out *chatResponse) (textBytes int, err error) {
	for i, choice := range choices {
		index := i
		if choice.Index != nil {
			index = *choice.Index
		}
		if index != 0 {
			return 0, errors.New("tencent: invalid choice index")
		}
		if !s.stream && choice.FinishReason == "" {
			return 0, io.ErrUnexpectedEOF
		}
		s.seen = true
		message := choice.Message
		if s.stream {
			message = choice.Delta
		}
		if s.finished && (message.Content != "" || message.Reasoning != "") {
			s.ended = true
			return 0, errors.New("tencent: output after finished choice")
		}
		textBytes += len(message.Content) + len(message.Reasoning)
		delta := chatMessage{Content: message.Content, Reasoning: message.Reasoning}
		if !s.semantic {
			delta.Role = "assistant"
		}
		converted := chatChoice{Index: index}
		if s.stream {
			converted.Delta = &delta
		} else {
			delta.Role = "assistant"
			converted.Message = &delta
		}
		if choice.FinishReason != "" {
			finish := choice.FinishReason
			if finish == "sensitive" {
				finish = "content_filter"
			}
			converted.FinishReason = &finish
			s.finished = true
		}
		out.Choices = append(out.Choices, converted)
	}
	return textBytes, nil
}

// emptyOutputEvent decides how to react when a frame carried no text: a
// non-streaming or already-finished response has no further output coming
// and is therefore an error, while a mid-stream frame may still just be
// carrying usage data.
func (s *responseStream) emptyOutputEvent(usage *gateway.Usage) (gateway.Event, error) {
	if !s.stream || s.finished {
		s.ended = true
		return gateway.Event{Kind: gateway.EventError, Err: upstream("empty_response", "Tencent returned no output"), Usage: usage}, nil
	}
	if usage != nil {
		return gateway.Event{Kind: gateway.EventUsage, Usage: usage}, nil
	}
	return gateway.Event{}, nil
}

func (s *responseStream) Close() error { return s.body.Close() }
