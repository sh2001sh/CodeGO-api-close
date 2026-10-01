package cloudflare

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

type nativeError struct {
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
}

type nativeUsage struct {
	Prompt     *int64 `json:"prompt_tokens"`
	Completion *int64 `json:"completion_tokens"`
	Total      *int64 `json:"total_tokens"`
	Details    *struct {
		Cached *int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

type nativeResponse struct {
	Result    json.RawMessage `json:"result"`
	Success   *bool           `json:"success"`
	Errors    []nativeError   `json:"errors"`
	Error     json.RawMessage `json:"error"`
	Response  *string         `json:"response"`
	Usage     *nativeUsage    `json:"usage"`
	ToolCalls json.RawMessage `json:"tool_calls"`
	Message   string          `json:"message"`
	Code      json.RawMessage `json:"code"`
}

type responseStream struct {
	body      io.ReadCloser
	reader    *sse.Reader
	stream    bool
	model     string
	id        string
	created   int64
	wantUsage bool
	delivered bool
	finished  bool
	usage     *gateway.Usage
	queue     []gateway.Event
}

func (s *responseStream) Close() error { return s.body.Close() }

func (s *responseStream) Next() (gateway.Event, error) {
	for {
		if len(s.queue) != 0 {
			event := s.queue[0]
			s.queue = s.queue[1:]
			return event, nil
		}
		if s.finished {
			return gateway.Event{}, io.EOF
		}
		data, name, err := s.read()
		if err != nil {
			s.finished = true
			return gateway.Event{}, err
		}
		if len(bytes.TrimSpace(data)) == 0 {
			continue
		}
		if s.stream && bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
			return s.handleDone()
		}
		event, cont, err := s.handleFrame(data, name)
		if cont {
			continue
		}
		return event, err
	}
}

// handleDone emits the terminal chunk (and trailing usage/done events) when
// the native stream signals completion with [DONE].
func (s *responseStream) handleDone() (gateway.Event, error) {
	s.finished = true
	if !s.delivered {
		return responseError("empty_response", "Cloudflare returned no output"), nil
	}
	finish, err := s.chunk("", "stop")
	if err != nil {
		return gateway.Event{}, err
	}
	if s.wantUsage && s.usage != nil {
		payload, err := json.Marshal(s.output("chat.completion.chunk", []any{}, s.usage))
		if err != nil {
			return gateway.Event{}, err
		}
		s.queue = append(s.queue, gateway.Event{Kind: gateway.EventData, Payload: payload, Usage: s.usage})
	}
	s.queue = append(s.queue, gateway.Event{Kind: gateway.EventDone})
	return finish, nil
}

// handleFrame decodes and converts a single native response/stream frame.
// cont reports that Next should loop again without returning (the frame
// carried only non-generated lifecycle data).
func (s *responseStream) handleFrame(data []byte, name string) (event gateway.Event, cont bool, err error) {
	in, upstreamErr := parseResponse(data)
	usage, err := usageOf(in.Usage)
	if err != nil {
		s.finished = true
		return responseError("invalid_response", err.Error()), false, nil
	}
	if upstreamErr != nil {
		s.finished = true
		return gateway.Event{Kind: gateway.EventError, Err: upstreamErr, Usage: usage}, false, nil
	}
	if strings.EqualFold(name, "error") {
		s.finished = true
		return gateway.Event{Kind: gateway.EventError, Err: cloudflareError(nativeError{Code: in.Code, Message: in.Message}), Usage: usage}, false, nil
	}
	if usage != nil {
		s.usage = usage
	}
	if !s.stream {
		s.finished = true
		if in.Response == nil {
			return responseError("invalid_response", "Cloudflare response has no response text"), false, nil
		}
		if *in.Response == "" {
			return responseError("empty_response", "Cloudflare returned no output"), false, nil
		}
		event, err = s.single(*in.Response, usage)
		return event, false, err
	}
	if in.Response != nil && *in.Response != "" {
		event, err = s.chunk(*in.Response, "")
		event.Usage = usage
		s.delivered = true
		return event, false, err
	}
	if usage != nil {
		return gateway.Event{Kind: gateway.EventUsage, Usage: usage}, false, nil
	}
	if in.Response == nil {
		s.finished = true
		return responseError("invalid_response", "Cloudflare stream event has neither response text nor usage"), false, nil
	}
	// An empty response prefix is lifecycle data, not generated output.
	return gateway.Event{}, true, nil
}

func (s *responseStream) read() ([]byte, string, error) {
	if s.stream {
		event, err := s.reader.Next()
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF // Only [DONE] ends a native Workers AI stream.
		}
		return event.Data, string(event.Name), err
	}
	data, err := io.ReadAll(io.LimitReader(s.body, maxJSONBody+1))
	if err == nil && len(data) > maxJSONBody {
		err = fmt.Errorf("cloudflare: response exceeds %d bytes", maxJSONBody)
	}
	if err == nil && len(data) == 0 {
		err = io.ErrUnexpectedEOF
	}
	return data, "", err
}

func usageOf(in *nativeUsage) (*gateway.Usage, error) {
	if in == nil {
		return nil, nil
	}
	for _, count := range []*int64{in.Prompt, in.Completion, in.Total} {
		if count != nil && *count < 0 {
			return nil, fmt.Errorf("cloudflare: negative token counts")
		}
	}
	if in.Details != nil && in.Details.Cached != nil && *in.Details.Cached < 0 {
		return nil, fmt.Errorf("cloudflare: negative cached token counts")
	}
	if in.Prompt == nil || in.Completion == nil {
		return nil, nil // Partial counts cannot establish actual total usage.
	}
	if *in.Prompt > math.MaxInt64-*in.Completion {
		return nil, fmt.Errorf("cloudflare: token total overflows")
	}
	if in.Total != nil && *in.Total != *in.Prompt+*in.Completion {
		return nil, fmt.Errorf("cloudflare: inconsistent token counts")
	}
	usage := &gateway.Usage{PromptTokens: *in.Prompt, CompletionTokens: *in.Completion}
	if in.Details != nil && in.Details.Cached != nil {
		if *in.Details.Cached > *in.Prompt {
			return nil, fmt.Errorf("cloudflare: invalid cached token counts")
		}
		usage.CachedTokens = *in.Details.Cached
	}
	return usage, nil
}

func usageJSON(usage *gateway.Usage) map[string]any {
	out := map[string]any{"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens,
		"total_tokens": usage.PromptTokens + usage.CompletionTokens}
	if usage.CachedTokens != 0 {
		out["prompt_tokens_details"] = map[string]int64{"cached_tokens": usage.CachedTokens}
	}
	return out
}

func (s *responseStream) output(object string, choices any, usage *gateway.Usage) map[string]any {
	out := map[string]any{"id": s.id, "object": object, "created": s.created, "model": s.model, "choices": choices}
	if usage != nil {
		out["usage"] = usageJSON(usage)
	}
	return out
}

func (s *responseStream) chunk(content, finish string) (gateway.Event, error) {
	delta := map[string]string{}
	if content != "" {
		delta["content"] = content
		if !s.delivered {
			delta["role"] = "assistant"
		}
	}
	choice := map[string]any{"index": 0, "delta": delta, "finish_reason": nil}
	if finish != "" {
		choice["finish_reason"] = finish
	}
	payload, err := json.Marshal(s.output("chat.completion.chunk", []any{choice}, nil))
	return gateway.Event{Kind: gateway.EventData, Payload: payload, TextBytes: len(content)}, err
}

func (s *responseStream) single(content string, usage *gateway.Usage) (gateway.Event, error) {
	choice := map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": content}, "finish_reason": "stop"}
	payload, err := json.Marshal(s.output("chat.completion", []any{choice}, usage))
	return gateway.Event{Kind: gateway.EventData, Payload: payload, Usage: usage, TextBytes: len(content)}, err
}
