package responses

import (
	"fmt"
	"io"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type chatStream struct {
	source                  gateway.EventStream
	id, model               string
	created                 int64
	streaming, includeUsage bool
	started, ended          bool
	queue                   []gateway.Event
	textBytes               map[string]int
	tools                   map[int]*chatCall
	toolIDs                 map[string]int
}

type chatCall struct {
	index, argumentBytes int
	id, name             string
}

func newChatStream(req *gateway.Request, source gateway.EventStream) *chatStream {
	s := &chatStream{source: source, id: "chatcmpl-" + req.ID, model: req.Model, streaming: req.Stream,
		includeUsage: gjson.GetBytes(req.Body, "stream_options.include_usage").Bool(),
		textBytes:    make(map[string]int), tools: make(map[int]*chatCall), toolIDs: make(map[string]int)}
	if !req.Received.IsZero() {
		s.created = req.Received.Unix()
	}
	return s
}

func (s *chatStream) Next() (gateway.Event, error) {
	for {
		if len(s.queue) > 0 {
			ev := s.queue[0]
			s.queue[0] = gateway.Event{}
			s.queue = s.queue[1:]
			return ev, nil
		}
		if s.ended {
			return gateway.Event{}, io.EOF
		}
		ev, err := s.source.Next()
		if err != nil {
			return ev, err
		}
		if ev.Kind == gateway.EventDone {
			s.ended = true
			return ev, nil
		}
		if ev.Kind == gateway.EventError {
			s.ended = true
			ev.Name, ev.Payload = "", nil
			return ev, nil
		}
		if ev.Kind == gateway.EventUsage {
			return ev, nil
		}
		root, failure := parseBody(ev.Payload)
		if failure != nil {
			return gateway.Event{Kind: gateway.EventError, Err: failure, Usage: ev.Usage}, nil
		}
		if ev.Name == "" {
			if !s.streaming {
				s.ended = true
				return s.complete(ev, root), nil
			}
			if failure = s.finish(ev, root); failure == nil {
				s.queue = append(s.queue, gateway.Event{Kind: gateway.EventDone})
				s.ended = true
			}
		} else {
			failure = s.convert(ev, root)
		}
		if failure != nil {
			s.queue = nil
			s.ended = true
			return gateway.Event{Kind: gateway.EventError, Err: failure, Usage: ev.Usage}, nil
		}
	}
}

func (s *chatStream) convert(ev gateway.Event, root gjson.Result) *gateway.UpstreamError {
	s.updateMetadata(root.Get("response"))
	outputIndex, contentIndex := int(root.Get("output_index").Int()), int(root.Get("content_index").Int())
	switch ev.Name {
	case "response.output_text.delta", "response.refusal.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		field, category := "content", "text"
		if strings.Contains(ev.Name, "refusal") {
			field, category = "refusal", "refusal"
		} else if strings.Contains(ev.Name, "reasoning") {
			field, category = "reasoning_content", "reasoning_text"
			if strings.Contains(ev.Name, "summary") {
				category = "reasoning_summary"
				contentIndex = int(root.Get("summary_index").Int())
			}
		}
		delta := root.Get("delta").Str
		if delta != "" {
			s.textBytes[partKey(category, outputIndex, contentIndex)] += len(delta)
			s.queue = append(s.queue, s.chunk(map[string]any{field: delta}, nil, len(delta)))
		}
	case "response.output_item.added", "response.output_item.done":
		if failure := s.outputItem(root.Get("item"), outputIndex, ev.Name == "response.output_item.done"); failure != nil {
			return failure
		}
	case "response.function_call_arguments.delta":
		if failure := s.callDelta(root, outputIndex); failure != nil {
			return failure
		}
	case "response.completed", "response.incomplete":
		return s.finish(ev, root.Get("response"))
	default:
		if strings.HasSuffix(ev.Name, ".delta") && root.Get("delta").Str != "" {
			return chatOutputError()
		}
	}
	if ev.Usage != nil {
		s.queue = append(s.queue, gateway.Event{Kind: gateway.EventUsage, Usage: ev.Usage})
	}
	return nil
}

func (s *chatStream) outputItem(item gjson.Result, index int, complete bool) *gateway.UpstreamError {
	switch item.Get("type").Str {
	case "function_call":
		return s.callItem(item, index, complete)
	case "message", "reasoning":
		if !complete {
			return nil
		}
		for _, path := range []string{"content", "summary"} {
			for contentIndex, part := range item.Get(path).Array() {
				field, category, value := "content", "text", part.Get("text").Str
				if item.Get("type").Str == "reasoning" {
					field, category = "reasoning_content", "reasoning_"+path
					if path == "content" {
						category = "reasoning_text"
					}
				} else if part.Get("type").Str == "refusal" {
					field, category, value = "refusal", "refusal", part.Get("refusal").Str
				} else if part.Get("type").Str != "output_text" {
					return chatOutputError()
				}
				key := partKey(category, index, contentIndex)
				if count := s.textBytes[key]; len(value) > count {
					suffix := value[count:]
					s.queue = append(s.queue, s.chunk(map[string]any{field: suffix}, nil, len(suffix)))
					s.textBytes[key] = len(value)
				}
			}
		}
		return nil
	default:
		return chatOutputError()
	}
}

func (s *chatStream) finish(ev gateway.Event, root gjson.Result) *gateway.UpstreamError {
	s.updateMetadata(root)
	for index, item := range root.Get("output").Array() {
		if failure := s.outputItem(item, index, true); failure != nil {
			return failure
		}
	}
	finish := s.chunk(map[string]any{}, chatFinish(root, len(s.tools) > 0), 0)
	finish.Usage = ev.Usage
	s.queue = append(s.queue, finish)
	if ev.Usage != nil {
		usage := gateway.Event{Kind: gateway.EventUsage, Usage: ev.Usage}
		if s.includeUsage {
			usage.Kind = gateway.EventData
			out := s.envelope("chat.completion.chunk", []any{})
			out["usage"] = chatUsage(root.Get("usage"), ev.Usage)
			usage.Payload = marshalChat(out)
		}
		s.queue = append(s.queue, usage)
	}
	return nil
}

func partKey(category string, outputIndex, contentIndex int) string {
	return fmt.Sprintf("%s/%d/%d", category, outputIndex, contentIndex)
}

func (s *chatStream) Close() error { return s.source.Close() }
