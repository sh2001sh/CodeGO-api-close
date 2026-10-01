package xunfei

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
	"github.com/tidwall/gjson"
)

func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	return &responseStream{body: resp.Body, reader: sse.NewReader(resp.Body, maxFrame), stream: req.Stream,
		wantUsage: gjson.GetBytes(req.Body, "stream_options.include_usage").Bool(), model: req.Model, id: "chatcmpl-" + req.ID,
		created: req.Received.Unix(), lastSeq: -1}
}

type responseStream struct {
	body                                io.ReadCloser
	reader                              *sse.Reader
	stream, wantUsage, ended, delivered bool
	model, id                           string
	created                             int64
	lastSeq                             int64
	text                                strings.Builder
	usage                               *gateway.Usage
	pending                             []gateway.Event
}

func (s *responseStream) Next() (gateway.Event, error) {
	for {
		if len(s.pending) > 0 {
			event := s.pending[0]
			s.pending[0] = gateway.Event{}
			s.pending = s.pending[1:]
			return event, nil
		}
		if s.ended {
			return gateway.Event{}, io.EOF
		}
		event, err := s.reader.Next()
		if err != nil {
			s.ended = true
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return gateway.Event{}, err
		}
		text, terminal, usage, errEvent, err := s.parseFrame(event.Data)
		if err != nil {
			s.ended = true
			return gateway.Event{}, err
		}
		if errEvent != nil {
			s.ended = true
			return *errEvent, nil
		}
		if usage != nil {
			s.usage = usage
		}
		if !s.stream {
			done, result, err := s.collect(text, terminal)
			if done {
				return result, err
			}
			continue
		}
		if terminal {
			return s.finish(text)
		}
		if text != "" {
			return s.chunk(text, nil)
		}
		if usage != nil {
			return gateway.Event{Kind: gateway.EventUsage, Usage: usage}, nil
		}
	}
}

// parseFrame validates and extracts the fields of a single SSE frame.
// errEvent carries an upstream-reported error to return as-is; err signals
// a malformed frame that must end the stream.
func (s *responseStream) parseFrame(data []byte) (text string, terminal bool, usage *gateway.Usage, errEvent *gateway.Event, err error) {
	root := gjson.ParseBytes(data)
	if !gjson.ValidBytes(data) || !root.IsObject() || root.Get("header.code").Type != gjson.Number || float64(root.Get("header.code").Int()) != root.Get("header.code").Float() {
		return "", false, nil, nil, errors.New("xunfei: malformed response header")
	}
	if code := root.Get("header.code").Int(); code != 0 {
		status := http.StatusBadGateway
		if code == 10013 || code == 11200 {
			status = http.StatusTooManyRequests
		}
		// Upstream messages can reflect signed credentials; return a safe code.
		ev := gateway.Event{Kind: gateway.EventError, Err: &gateway.UpstreamError{Status: status, Type: "upstream_error", Code: "spark_" + strconv.FormatInt(code, 10), Message: "Spark upstream reported an error"}}
		return "", false, nil, &ev, nil
	}
	status := root.Get("payload.choices.status")
	if status.Type != gjson.Number || status.Int() < 0 || status.Int() > 2 || float64(status.Int()) != status.Float() || !root.Get("payload.choices.text").IsArray() {
		return "", false, nil, nil, errors.New("xunfei: malformed response choices")
	}
	if seq := root.Get("payload.choices.seq"); seq.Exists() {
		if seq.Type != gjson.Number || seq.Int() <= s.lastSeq || seq.Int() < 0 || float64(seq.Int()) != seq.Float() {
			return "", false, nil, nil, errors.New("xunfei: invalid response sequence")
		}
		s.lastSeq = seq.Int()
	}
	if sid := root.Get("header.sid").Str; sid != "" {
		s.id = sid
	}
	for _, item := range root.Get("payload.choices.text").Array() {
		if item.Get("content").Type != gjson.String || (item.Get("index").Exists() && (item.Get("index").Type != gjson.Number || item.Get("index").Float() != 0)) {
			return "", false, nil, nil, errors.New("xunfei: invalid text choice")
		}
		text += item.Get("content").Str
	}
	usage, err = parseUsage(root.Get("payload.usage.text"))
	if err != nil {
		return "", false, nil, nil, err
	}
	return text, status.Int() == 2, usage, nil, nil
}

// collect accumulates text for a non-streaming response. done reports
// whether Next should return result/err immediately rather than loop again.
func (s *responseStream) collect(text string, terminal bool) (done bool, result gateway.Event, err error) {
	if s.text.Len()+len(text) > maxOutput {
		s.ended = true
		return true, gateway.Event{}, sse.ErrEventTooLarge
	}
	s.text.WriteString(text)
	if !terminal {
		return false, gateway.Event{}, nil
	}
	s.ended = true
	if s.text.Len() == 0 {
		result, err = s.empty()
		return true, result, err
	}
	result, err = s.completion(s.text.String())
	return true, result, err
}

// finish emits the terminal chunk of a streaming response, queuing the
// trailing usage and done events for subsequent Next calls.
func (s *responseStream) finish(text string) (gateway.Event, error) {
	s.ended = true
	if !s.delivered && text == "" {
		return s.empty()
	}
	finish := "stop"
	chunk, err := s.chunk(text, &finish)
	if err != nil {
		return gateway.Event{}, err
	}
	if s.usage != nil {
		usageEvent, err := s.usageEvent()
		if err != nil {
			return gateway.Event{}, err
		}
		s.pending = append(s.pending, usageEvent)
	}
	s.pending = append(s.pending, gateway.Event{Kind: gateway.EventDone})
	return chunk, nil
}

func parseUsage(root gjson.Result) (*gateway.Usage, error) {
	if !root.Exists() || root.Type == gjson.Null {
		return nil, nil
	}
	if !root.IsObject() {
		return nil, errors.New("xunfei: invalid usage")
	}
	if !root.Get("prompt_tokens").Exists() && !root.Get("completion_tokens").Exists() {
		return nil, nil
	}
	if !root.Get("prompt_tokens").Exists() || !root.Get("completion_tokens").Exists() {
		return nil, errors.New("xunfei: incomplete token usage")
	}
	for _, field := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
		value := root.Get(field)
		if value.Exists() && (value.Type != gjson.Number || value.Int() < 0 || float64(value.Int()) != value.Float()) {
			return nil, errors.New("xunfei: invalid token usage")
		}
	}
	if root.Get("prompt_tokens").Int() > math.MaxInt64-root.Get("completion_tokens").Int() {
		return nil, errors.New("xunfei: token usage overflow")
	}
	return &gateway.Usage{PromptTokens: root.Get("prompt_tokens").Int(), CompletionTokens: root.Get("completion_tokens").Int()}, nil
}

func (s *responseStream) empty() (gateway.Event, error) {
	if s.usage != nil {
		s.pending = append(s.pending, gateway.Event{Kind: gateway.EventDone})
		return gateway.Event{Kind: gateway.EventUsage, Usage: s.usage}, nil
	}
	return gateway.Event{Kind: gateway.EventDone}, nil
}

func (s *responseStream) chunk(text string, finish *string) (gateway.Event, error) {
	delta := map[string]string{}
	if text != "" {
		delta["content"] = text
	}
	if !s.delivered {
		delta["role"] = "assistant"
		s.delivered = true
	}
	choice := map[string]any{"index": 0, "delta": delta, "finish_reason": finish}
	return s.output("chat.completion.chunk", []any{choice}, nil, len(text))
}

func (s *responseStream) completion(text string) (gateway.Event, error) {
	choice := map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": text}, "finish_reason": "stop"}
	return s.output("chat.completion", []any{choice}, s.usage, len(text))
}

func (s *responseStream) usageEvent() (gateway.Event, error) {
	if !s.wantUsage {
		return gateway.Event{Kind: gateway.EventUsage, Usage: s.usage}, nil
	}
	return s.output("chat.completion.chunk", []any{}, s.usage, 0)
}

func (s *responseStream) output(object string, choices []any, usage *gateway.Usage, textBytes int) (gateway.Event, error) {
	response := map[string]any{"id": s.id, "object": object, "created": s.created, "model": s.model, "choices": choices}
	if usage != nil {
		response["usage"] = map[string]int64{"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens, "total_tokens": usage.PromptTokens + usage.CompletionTokens}
	}
	payload, err := json.Marshal(response)
	return gateway.Event{Kind: gateway.EventData, Payload: payload, Usage: s.usage, TextBytes: textBytes}, err
}

func (s *responseStream) Close() error { return s.body.Close() }
