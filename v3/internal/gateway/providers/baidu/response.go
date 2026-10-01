package baidu

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
	"github.com/tidwall/gjson"
)

type responseStream struct {
	body      io.ReadCloser
	reader    *sse.Reader
	stream    bool
	wantUsage bool
	delivered bool
	ended     bool
	id        string
	model     string
	created   int64
	pending   []gateway.Event
}

func newResponseStream(req *gateway.Request, resp *http.Response) *responseStream {
	s := &responseStream{body: resp.Body, stream: req.Stream, model: req.Model,
		id: "chatcmpl-" + req.ID, created: req.Received.Unix(),
		wantUsage: gjson.GetBytes(req.Body, "stream_options.include_usage").Bool()}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		s.reader = sse.NewReader(resp.Body, 0)
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
		data, err := s.read()
		if err != nil {
			s.ended = true
			return gateway.Event{}, err
		}
		event, cont, err := s.handleFrame(data)
		if cont {
			continue
		}
		return event, err
	}
}

// handleFrame decodes and converts a single native response/stream frame.
// cont reports that Next should loop again without returning (the frame
// carried only lifecycle data such as a bare usage update).
func (s *responseStream) handleFrame(data []byte) (event gateway.Event, cont bool, err error) {
	if !gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject() {
		s.ended = true
		return gateway.Event{Kind: gateway.EventError, Err: upstream("invalid_response", "baidu: invalid response JSON")}, false, nil
	}
	root := gjson.ParseBytes(data)
	usage, err := parseUsage(root.Get("usage"))
	if err != nil {
		s.ended = true
		return gateway.Event{}, false, err
	}
	if code := root.Get("error_code"); code.Int() != 0 || root.Get("error_msg").Str != "" {
		s.ended = true
		return gateway.Event{Kind: gateway.EventError, Err: upstream("baidu_"+code.String(), root.Get("error_msg").Str), Usage: usage}, false, nil
	}
	if id := root.Get("id").Str; id != "" {
		s.id = id
	}
	if created := root.Get("created"); created.Exists() {
		s.created = created.Int()
	}
	result := root.Get("result")
	if result.Exists() && result.Type != gjson.String {
		s.ended = true
		return gateway.Event{}, false, errors.New("baidu: result must be text")
	}
	finished := s.reader == nil || root.Get("is_end").Bool()
	if result.Str != "" || (finished && s.delivered) {
		finish := ""
		if finished {
			finish = "stop"
			if root.Get("is_truncated").Bool() {
				finish = "length"
			}
		}
		event, err = s.output(result.Str, finish, usage)
		if finished {
			s.finish(usage)
		}
		return event, false, err
	}
	if finished {
		s.ended = true
		s.pending = append(s.pending, gateway.Event{Kind: gateway.EventDone})
	}
	if usage != nil {
		return gateway.Event{Kind: gateway.EventUsage, Usage: usage}, false, nil
	}
	return gateway.Event{}, true, nil
}

func (s *responseStream) read() ([]byte, error) {
	if s.reader != nil {
		for {
			ev, err := s.reader.Next()
			if errors.Is(err, io.EOF) && s.delivered {
				err = io.ErrUnexpectedEOF
			}
			if err != nil {
				return nil, err
			}
			if len(ev.Data) > 0 {
				return ev.Data, nil
			}
		}
	}
	data, err := io.ReadAll(io.LimitReader(s.body, (64<<20)+1))
	if len(data) > 64<<20 {
		return nil, sse.ErrEventTooLarge
	}
	return data, err
}

func (s *responseStream) output(text, finish string, usage *gateway.Usage) (gateway.Event, error) {
	message := map[string]any{"content": text}
	if !s.delivered || !s.stream {
		message["role"] = "assistant"
	}
	s.delivered = true
	choice := map[string]any{"index": 0, "finish_reason": nil}
	object := "chat.completion"
	if s.stream {
		choice["delta"] = message
		object += ".chunk"
	} else {
		choice["message"] = message
	}
	if finish != "" {
		choice["finish_reason"] = finish
	}
	body := map[string]any{"id": s.id, "object": object, "created": s.created, "model": s.model, "choices": []any{choice}}
	if !s.stream && usage != nil {
		body["usage"] = usageJSON(usage)
	}
	data, err := json.Marshal(body)
	return gateway.Event{Kind: gateway.EventData, Payload: data, Usage: usage, TextBytes: len(text)}, err
}

func (s *responseStream) finish(usage *gateway.Usage) {
	s.ended = true
	if s.stream && s.wantUsage && usage != nil {
		data, _ := json.Marshal(map[string]any{"id": s.id, "object": "chat.completion.chunk", "created": s.created,
			"model": s.model, "choices": []any{}, "usage": usageJSON(usage)})
		s.pending = append(s.pending, gateway.Event{Kind: gateway.EventData, Payload: data, Usage: usage})
	}
	if s.stream {
		s.pending = append(s.pending, gateway.Event{Kind: gateway.EventDone})
	}
}

func (s *responseStream) Close() error { return s.body.Close() }

func parseUsage(root gjson.Result) (*gateway.Usage, error) {
	if !root.Exists() || root.Type == gjson.Null {
		return nil, nil
	}
	if !root.IsObject() {
		return nil, errors.New("baidu: invalid usage object")
	}
	prompt, completion, total := root.Get("prompt_tokens"), root.Get("completion_tokens"), root.Get("total_tokens")
	if !prompt.Exists() || (!completion.Exists() && !total.Exists()) {
		return nil, nil
	}
	counts := make([]int64, 3)
	for i, value := range []gjson.Result{prompt, completion, total} {
		if !value.Exists() {
			continue
		}
		count, err := strconv.ParseInt(value.Raw, 10, 64)
		if value.Type != gjson.Number || err != nil || count < 0 {
			return nil, errors.New("baidu: invalid usage count")
		}
		counts[i] = count
	}
	count := counts[1]
	if !completion.Exists() {
		count = counts[2] - counts[0]
		if count < 0 {
			return nil, errors.New("baidu: total usage is lower than prompt usage")
		}
	}
	if counts[0] > 1<<63-1-count {
		return nil, errors.New("baidu: usage count overflow")
	}
	return &gateway.Usage{PromptTokens: counts[0], CompletionTokens: count}, nil
}

func usageJSON(u *gateway.Usage) map[string]int64 {
	return map[string]int64{"prompt_tokens": u.PromptTokens, "completion_tokens": u.CompletionTokens, "total_tokens": u.PromptTokens + u.CompletionTokens}
}
