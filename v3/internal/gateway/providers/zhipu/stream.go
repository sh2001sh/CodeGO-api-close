package zhipu

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type responseStream struct {
	body              io.ReadCloser
	reader            *nativeReader
	stream, wantUsage bool
	ended, delivered  bool
	id, model         string
	created           int64
	usage             *gateway.Usage
	pending           []gateway.Event
}

func newResponseStream(req *gateway.Request, resp *http.Response) *responseStream {
	s := &responseStream{body: resp.Body, stream: req.Stream, id: "chatcmpl-" + req.ID, model: req.Model,
		created: req.Received.Unix(), wantUsage: gjson.GetBytes(req.Body, "stream_options.include_usage").Bool()}
	if req.Stream {
		s.reader = &nativeReader{reader: bufio.NewReaderSize(resp.Body, 64<<10)}
	}
	return s
}

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
		if !s.stream {
			return s.single()
		}
		event, err := s.reader.Next()
		if err != nil {
			s.ended = true
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF // EOF cannot substitute for native finish.
			}
			return gateway.Event{}, err
		}
		if len(event.meta) > 0 {
			if ev, err := s.metadata(event.meta); err != nil || ev.Kind != 0 {
				s.ended = true
				return ev, err
			}
		}
		ev, cont, err := s.handleEvent(event)
		if cont {
			continue
		}
		return ev, err
	}
}

// handleEvent dispatches a single native stream event by name. cont reports
// that Next should loop again without returning (the event produced no
// deliverable chunk, e.g. an empty "add" or an out-of-band "meta").
func (s *responseStream) handleEvent(event nativeEvent) (ev gateway.Event, cont bool, err error) {
	switch event.name {
	case "add":
		if len(event.data) > 0 {
			ev, err = s.chunk(string(event.data), nil, nil, false)
			return ev, false, err
		}
		return gateway.Event{}, true, nil
	case "finish":
		s.ended = true
		if len(event.data) > 0 {
			chunkEv, err := s.chunk(string(event.data), nil, nil, false)
			if err != nil {
				return gateway.Event{}, false, err
			}
			s.pending = append(s.pending, chunkEv)
		}
		if err := s.finish(); err != nil {
			return gateway.Event{}, false, err
		}
		return gateway.Event{}, true, nil
	case "meta":
		if ev, err := s.metadata(event.data); err != nil || ev.Kind != 0 {
			s.ended = true
			return ev, false, err
		}
		// A separate metadata event carries accounting, not a completion marker.
		return gateway.Event{}, true, nil
	case "error", "interrupted":
		s.ended = true
		s.pending = nil
		e := nativeError(gjson.ParseBytes(event.data))
		if e == nil {
			message := string(event.data)
			if message == "" {
				message = "Zhipu stream reported an error"
			}
			e = upstream("zhipu_error", message)
		}
		return gateway.Event{Kind: gateway.EventError, Err: e, Usage: s.usage}, false, nil
	case "":
		if len(event.data) != 0 {
			s.ended = true
			s.pending = nil
			return gateway.Event{Kind: gateway.EventError, Err: upstream("invalid_response", "zhipu: stream data has no native event type")}, false, nil
		}
		return gateway.Event{}, true, nil
	default:
		s.ended = true
		s.pending = nil
		return gateway.Event{Kind: gateway.EventError, Err: upstream("invalid_response", "zhipu: unsupported native stream event")}, false, nil
	}
}

func (s *responseStream) metadata(data []byte) (gateway.Event, error) {
	root := gjson.ParseBytes(data)
	if !gjson.ValidBytes(data) || !root.IsObject() {
		return gateway.Event{}, errors.New("zhipu: invalid stream metadata")
	}
	usage, err := parseUsage(root.Get("usage"))
	if err != nil {
		return gateway.Event{}, err
	}
	if usage != nil {
		s.usage = usage
	}
	s.updateID(root)
	if e := nativeError(root); e != nil {
		return gateway.Event{Kind: gateway.EventError, Err: e, Usage: s.usage}, nil
	}
	if e := statusError(root.Get("task_status").Str); e != nil {
		return gateway.Event{Kind: gateway.EventError, Err: e, Usage: s.usage}, nil
	}
	// Account for metadata immediately so a later cut/error retains exact usage.
	if usage != nil {
		s.pending = append(s.pending, gateway.Event{Kind: gateway.EventUsage, Usage: usage})
	}
	return gateway.Event{}, nil
}

func (s *responseStream) updateID(root gjson.Result) {
	if id := root.Get("task_id").Str; id != "" {
		s.id = id
	} else if id := root.Get("request_id").Str; id != "" {
		s.id = id
	}
}

func (s *responseStream) finish() error {
	if s.delivered {
		reason := "stop"
		ev, err := s.chunk("", &reason, nil, false)
		if err != nil {
			return err
		}
		s.pending = append(s.pending, ev)
	}
	if s.usage != nil && s.wantUsage && s.delivered {
		ev, err := s.chunk("", nil, s.usage, true)
		if err != nil {
			return err
		}
		s.pending = append(s.pending, ev)
	}
	s.pending = append(s.pending, gateway.Event{Kind: gateway.EventDone})
	return nil
}

func (s *responseStream) chunk(text string, finish *string, usage *gateway.Usage, usageOnly bool) (gateway.Event, error) {
	choices := []chatChoice{}
	if !usageOnly {
		message := &chatMessage{Content: text}
		if !s.delivered {
			message.Role = "assistant"
		}
		if text != "" {
			s.delivered = true
		}
		choices = append(choices, chatChoice{Delta: message, FinishReason: finish})
	}
	payload, err := json.Marshal(chatResponse{ID: s.id, Object: "chat.completion.chunk", Created: s.created, Model: s.model,
		Choices: choices, Usage: usageJSON(usage)})
	return gateway.Event{Kind: gateway.EventData, Payload: payload, TextBytes: len(text), Usage: usage}, err
}

func (s *responseStream) Close() error { return s.body.Close() }

var _ gateway.Provider = Provider{}
