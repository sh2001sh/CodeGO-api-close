package cohere

import (
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
	body                                io.ReadCloser
	reader                              *sse.Reader
	stream, wantUsage, delivered, ended bool
	id, model                           string
	created                             int64
	tools                               map[int]bool
	pending                             []gateway.Event
}

func newResponseStream(req *gateway.Request, resp *http.Response) *responseStream {
	s := &responseStream{body: resp.Body, stream: req.Stream, id: "chatcmpl-" + req.ID, model: req.Model,
		created: req.Received.Unix(), wantUsage: gjson.GetBytes(req.Body, "stream_options.include_usage").Bool(), tools: make(map[int]bool)}
	if req.Stream {
		s.reader = sse.NewReader(resp.Body, maxStreamEvent)
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
		data, name, err := s.read()
		if err != nil {
			s.ended = true
			return gateway.Event{}, err
		}
		root := gjson.ParseBytes(data)
		if !gjson.ValidBytes(data) || !root.IsObject() {
			s.ended = true
			return gateway.Event{Kind: gateway.EventError, Err: upstream("invalid_response", "cohere: invalid response JSON")}, nil
		}
		if failure := responseError(root); failure != nil {
			s.ended = true
			return gateway.Event{Kind: gateway.EventError, Err: failure}, nil
		}
		if !s.stream {
			s.ended = true
			return s.single(root)
		}
		if root.Get("type").Str != "" {
			name = root.Get("type").Str
		}
		ev, cont, err := s.handleStreamEvent(root, name)
		if cont {
			continue
		}
		return ev, err
	}
}

// handleStreamEvent dispatches a single native SSE event by name. cont
// reports that Next should loop again without returning (the event
// produced no deliverable chunk, e.g. a bookkeeping or citation event).
func (s *responseStream) handleStreamEvent(root gjson.Result, name string) (ev gateway.Event, cont bool, err error) {
	switch name {
	case "message-start":
		if id := root.Get("id").Str; id != "" {
			s.id = id
		}
		if id := root.Get("delta.message.id").Str; id != "" {
			s.id = id
		}
		return gateway.Event{}, true, nil
	case "content-start", "content-delta":
		text := root.Get("delta.message.content.text").Str
		if text == "" {
			return gateway.Event{}, true, nil
		}
		ev, err = s.chunk(chatMessage{Content: text}, nil, nil, false, len(text))
		return ev, false, err
	case "tool-plan-delta":
		text := root.Get("delta.message.tool_plan").Str
		if text == "" {
			return gateway.Event{}, true, nil
		}
		ev, err = s.chunk(chatMessage{Reasoning: text}, nil, nil, false, len(text))
		return ev, false, err
	case "tool-call-start", "tool-call-delta":
		ev, err = s.toolChunk(root, name)
		return ev, false, err
	case "content-end", "tool-call-end":
		return gateway.Event{}, true, nil
	case "message-end":
		if err := s.finish(root); err != nil {
			s.ended = true
			return gateway.Event{}, false, err
		}
		return gateway.Event{}, true, nil
	case "error":
		s.ended = true
		return gateway.Event{Kind: gateway.EventError, Err: upstream("cohere_error", "Cohere stream reported an error")}, false, nil
	default:
		if strings.HasPrefix(name, "citation-") || strings.HasPrefix(name, "search-") {
			return gateway.Event{}, true, nil
		}
		s.ended = true
		return gateway.Event{Kind: gateway.EventError, Err: upstream("invalid_response", "cohere: unsupported stream event")}, false, nil
	}
}

func (s *responseStream) read() ([]byte, string, error) {
	if s.stream {
		for {
			ev, err := s.reader.Next()
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			if err != nil {
				return nil, "", err
			}
			if len(ev.Data) > 0 {
				return ev.Data, string(ev.Name), nil
			}
		}
	}
	data, err := io.ReadAll(io.LimitReader(s.body, maxJSONBody+1))
	if err == nil && len(data) > maxJSONBody {
		err = sse.ErrEventTooLarge
	}
	return data, "", err
}

func (s *responseStream) toolChunk(root gjson.Result, name string) (gateway.Event, error) {
	index := int(root.Get("index").Int())
	if !root.Get("index").Exists() || index < 0 {
		return gateway.Event{}, errors.New("cohere: tool chunk has invalid index")
	}
	call := root.Get("delta.message.tool_calls")
	if call.IsArray() {
		call = call.Get("0")
	}
	c, err := parseCall(call)
	if err != nil {
		return gateway.Event{}, err
	}
	c.Index = &index
	if name == "tool-call-start" {
		if s.tools[index] || c.ID == "" || c.Function.Name == "" {
			return gateway.Event{}, errors.New("cohere: invalid or duplicate tool start")
		}
		s.tools[index] = true
		if c.Type == "" {
			c.Type = "function"
		}
	} else if !s.tools[index] {
		return gateway.Event{}, errors.New("cohere: tool delta precedes its start")
	}
	return s.chunk(chatMessage{ToolCalls: []toolCall{c}}, nil, nil, false, len(c.Function.Arguments))
}

func (s *responseStream) finish(root gjson.Result) error {
	s.ended = true
	usageRoot := root.Get("delta.usage")
	if !usageRoot.Exists() {
		usageRoot = root.Get("usage")
	}
	usage, err := parseUsage(usageRoot)
	if err != nil {
		return err
	}
	reason := root.Get("delta.finish_reason").Str
	if reason == "" {
		reason = root.Get("finish_reason").Str
	}
	if failedReason(reason) {
		s.pending = append(s.pending, gateway.Event{Kind: gateway.EventError, Err: generationError(reason), Usage: usage})
		return nil
	}
	if reason == "" {
		return errors.New("cohere: message-end has no finish reason")
	}
	if !s.delivered {
		s.pending = append(s.pending, gateway.Event{Kind: gateway.EventError, Err: upstream("empty_response", "Cohere returned no output"), Usage: usage})
		return nil
	}
	finish := finishReason(reason, len(s.tools) > 0)
	ev, err := s.chunk(chatMessage{}, &finish, nil, false, 0)
	if err != nil {
		return err
	}
	s.pending = append(s.pending, ev)
	if usage != nil {
		ev = gateway.Event{Kind: gateway.EventUsage, Usage: usage}
		if s.wantUsage {
			ev, err = s.chunk(chatMessage{}, nil, usage, true, 0)
			if err != nil {
				return err
			}
		}
		s.pending = append(s.pending, ev)
	}
	s.pending = append(s.pending, gateway.Event{Kind: gateway.EventDone})
	return nil
}

func (s *responseStream) chunk(delta chatMessage, reason *string, usage *gateway.Usage, usageOnly bool, textBytes int) (gateway.Event, error) {
	choices := []chatChoice{}
	if !usageOnly {
		if !s.delivered {
			delta.Role = "assistant"
			s.delivered = true
		}
		choices = append(choices, chatChoice{Delta: &delta, FinishReason: reason})
	}
	payload, err := json.Marshal(chatResponse{ID: s.id, Object: "chat.completion.chunk", Created: s.created, Model: s.model, Choices: choices, Usage: usageJSON(usage)})
	return gateway.Event{Kind: gateway.EventData, Payload: payload, Usage: usage, TextBytes: textBytes}, err
}

func (s *responseStream) Close() error { return s.body.Close() }
