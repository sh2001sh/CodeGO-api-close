// Package openai adapts OpenAI-compatible chat completions upstreams.
package openai

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

// ID is the provider id used in catalog channels.
const ID = "openai"

const maxJSONBody = 64 << 20

// Provider implements gateway.Provider.
type Provider struct {
	ChatPath           string // default /v1/chat/completions
	DisableStreamUsage bool   // for compatible upstreams without stream_options
}

// BuildRequest forwards the client body, rewriting only the model name and
// asking for usage in streams so billing never has to estimate.
func (p Provider) BuildRequest(ctx context.Context, req *gateway.Request, t gateway.Target) (*http.Request, error) {
	body := req.Body
	var err error
	if t.UpstreamModel != "" && t.UpstreamModel != req.Model {
		if body, err = sjson.SetBytes(body, "model", t.UpstreamModel); err != nil {
			return nil, err
		}
	}
	if req.Stream && !p.DisableStreamUsage && !gjson.GetBytes(body, "stream_options.include_usage").Bool() {
		if body, err = sjson.SetBytes(body, "stream_options.include_usage", true); err != nil {
			return nil, err
		}
	}

	path := p.ChatPath
	if path == "" {
		path = "/v1/chat/completions"
	}
	base := strings.TrimRight(t.BaseURL, "/")
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(path, "/v1/") {
		path = strings.TrimPrefix(path, "/v1")
	}
	url := base + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+t.Secret)
	if req.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	return httpReq, nil
}

// Decode returns an event stream for a 200 response.
func (Provider) Decode(req *gateway.Request, resp *http.Response) gateway.EventStream {
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		// Usage is always requested upstream; the usage-only chunk reaches the
		// client only if the client asked for it itself.
		wantUsage := gjson.GetBytes(req.Body, "stream_options.include_usage").Bool()
		return &stream{body: resp.Body, r: sse.NewReader(resp.Body, 0), forwardUsage: wantUsage, expectedChoices: max(1, int(gjson.GetBytes(req.Body, "n").Int()))}
	}
	return &single{body: resp.Body}
}

type stream struct {
	body            io.ReadCloser
	r               *sse.Reader
	forwardUsage    bool
	expectedChoices int
	finished        map[int]bool
	semantic        bool
	done            bool
	buffer          []gateway.Event
	queue           []gateway.Event
	bufferBytes     int
}

var doneMarker = []byte("[DONE]")

func (s *stream) nextRaw() (gateway.Event, error) {
	if s.done {
		return gateway.Event{}, io.EOF
	}
	ev, err := s.r.Next()
	if err != nil {
		if errors.Is(err, io.EOF) && s.semantic {
			if len(s.finished) >= s.expectedChoices {
				s.done = true
				return gateway.Event{Kind: gateway.EventDone}, nil
			}
			return gateway.Event{}, io.ErrUnexpectedEOF
		}
		return gateway.Event{}, err
	}
	data := ev.Data
	if bytes.Equal(data, doneMarker) {
		s.done = true
		return gateway.Event{Kind: gateway.EventDone}, nil
	}
	if !gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject() {
		return gateway.Event{}, errors.New("openai: invalid stream JSON")
	}
	if e := parseError(data); e != nil {
		return gateway.Event{Kind: gateway.EventError, Err: e, Usage: optionalUsage(data)}, nil
	}
	for _, choice := range gjson.GetBytes(data, "choices").Array() {
		finish := choice.Get("finish_reason")
		if finish.Exists() && finish.Type != gjson.Null && finish.Str != "" {
			if s.finished == nil {
				s.finished = make(map[int]bool)
			}
			s.finished[int(choice.Get("index").Int())] = true
		}
	}

	res := gjson.GetManyBytes(data, "usage", "choices.#")
	out := gateway.Event{Kind: gateway.EventData, Payload: data, TextBytes: generatedBytes(data, "delta")}
	if res[0].IsObject() {
		out.Usage = parseUsage(res[0])
		if res[1].Int() == 0 && !s.forwardUsage {
			out.Kind = gateway.EventUsage // usage-only chunk the client did not ask for
		}
	}
	return out, nil
}

func (s *stream) Close() error { return s.body.Close() }

// single yields a non-streaming JSON body as one data event.
type single struct {
	body io.ReadCloser
	done bool
}

func (s *single) Next() (gateway.Event, error) {
	if s.done {
		return gateway.Event{}, io.EOF
	}
	s.done = true
	data, err := io.ReadAll(io.LimitReader(s.body, maxJSONBody+1))
	if err != nil {
		return gateway.Event{}, err
	}
	if len(data) > maxJSONBody || !gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject() {
		return gateway.Event{}, errors.New("openai: invalid or oversized JSON response")
	}
	if e := parseError(data); e != nil {
		return gateway.Event{Kind: gateway.EventError, Err: e, Usage: optionalUsage(data)}, nil
	}
	out := gateway.Event{Kind: gateway.EventData, Payload: data, TextBytes: generatedBytes(data, "message")}
	if usage := gjson.GetBytes(data, "usage"); usage.IsObject() {
		out.Usage = parseUsage(usage)
	}
	if !meaningfulChoices(data, "message") {
		if out.Usage != nil {
			return gateway.Event{Kind: gateway.EventUsage, Usage: out.Usage}, nil
		}
		return gateway.Event{Kind: gateway.EventDone}, nil
	}
	return out, nil
}

func (s *single) Close() error { return s.body.Close() }

func optionalUsage(data []byte) *gateway.Usage {
	if usage := gjson.GetBytes(data, "usage"); usage.IsObject() {
		return parseUsage(usage)
	}
	return nil
}

func parseUsage(u gjson.Result) *gateway.Usage {
	return &gateway.Usage{
		PromptTokens:      u.Get("prompt_tokens").Int(),
		CompletionTokens:  u.Get("completion_tokens").Int(),
		CachedTokens:      u.Get("prompt_tokens_details.cached_tokens").Int(),
		CacheWriteTokens:  u.Get("prompt_tokens_details.cache_creation_tokens").Int(),
		ImageInputTokens:  u.Get("prompt_tokens_details.image_tokens").Int(),
		ImageOutputTokens: u.Get("completion_tokens_details.image_tokens").Int(),
		AudioInputTokens:  u.Get("prompt_tokens_details.audio_tokens").Int(),
		AudioOutputTokens: u.Get("completion_tokens_details.audio_tokens").Int(),
	}
}

// parseError recognises OpenAI's in-band {"error": {...}} objects.
func parseError(data []byte) *gateway.UpstreamError {
	e := gjson.GetBytes(data, "error")
	if !e.Exists() || e.Type == gjson.Null {
		return nil
	}
	msg := e.Get("message").Str
	if msg == "" {
		msg = e.String()
	}
	return &gateway.UpstreamError{
		Status:  http.StatusBadGateway,
		Type:    firstNonEmpty(e.Get("type").Str, "upstream_error"),
		Code:    firstNonEmpty(e.Get("code").String(), "upstream_error"),
		Message: msg,
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
