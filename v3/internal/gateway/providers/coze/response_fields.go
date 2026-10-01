package coze

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func nativeUsage(root gjson.Result) (*gateway.Usage, error) {
	if !root.Exists() || root.Type == gjson.Null {
		return nil, nil
	}
	if !root.IsObject() {
		return nil, errors.New("coze: usage must be an object")
	}
	input, output, total := root.Get("input_count"), root.Get("output_count"), root.Get("token_count")
	if !input.Exists() && !output.Exists() && !total.Exists() {
		return nil, nil
	}
	for _, value := range []gjson.Result{input, output} {
		if value.Type != gjson.Number || value.Int() < 0 || float64(value.Int()) != value.Float() {
			return nil, errors.New("coze: usage requires nonnegative integer input_count and output_count")
		}
	}
	if input.Int() > (1<<63-1)-output.Int() {
		return nil, errors.New("coze: token counts overflow")
	}
	if total.Exists() && (total.Type != gjson.Number || total.Int() < 0 || float64(total.Int()) != total.Float() || total.Int() != input.Int()+output.Int()) {
		return nil, errors.New("coze: total token count is inconsistent")
	}
	return &gateway.Usage{PromptTokens: input.Int(), CompletionTokens: output.Int()}, nil
}

func (s *responseStream) envelope(object string) map[string]any {
	id := s.req.ID
	if id == "" {
		id = "coze"
	}
	created := s.req.Received.Unix()
	if s.req.Received.IsZero() {
		created = time.Now().Unix()
	}
	return map[string]any{"id": "chatcmpl-" + id, "object": object, "model": s.req.Model, "created": created}
}

func (s *responseStream) chunk(delta map[string]any, finish any, usage *gateway.Usage) []byte {
	out := s.envelope("chat.completion.chunk")
	if usage != nil {
		out["choices"] = []any{}
		out["usage"] = usageJSON(usage)
	} else {
		out["choices"] = []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}
	}
	payload, _ := json.Marshal(out)
	return payload
}

func usageJSON(u *gateway.Usage) map[string]int64 {
	return map[string]int64{"prompt_tokens": u.PromptTokens, "completion_tokens": u.CompletionTokens, "total_tokens": u.PromptTokens + u.CompletionTokens}
}

func (s *responseStream) failure(code, message string) gateway.Event {
	return gateway.Event{Kind: gateway.EventError, Err: upstream(code, message), Usage: s.usage}
}

func nativeErrorMessage(root gjson.Result) string {
	for _, path := range []string{"message", "msg", "last_error.message"} {
		if text := root.Get(path); text.Type == gjson.String && text.Str != "" {
			return text.Str
		}
	}
	if status := root.Get("status").Str; status != "" {
		return fmt.Sprintf("Coze chat status: %s", status)
	}
	return "Coze reported an upstream error"
}
