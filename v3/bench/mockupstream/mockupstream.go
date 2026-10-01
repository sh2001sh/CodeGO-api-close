// Package mockupstream is an OpenAI-compatible fake upstream for load tests
// and terminal-state tests. A request picks its scenario via query string
//
//	?scenario=complete&chunks=20&interval_ms=10&ttfb_ms=50
//
// or via a path prefix, so a channel base URL can pin a scenario:
//
//	/m/{scenario}[/c{chunks}][/i{interval_ms}][/t{ttfb_ms}]/v1/chat/completions
//
// Like OpenAI, it streams only when the JSON body has "stream": true.
// Scenarios map one-to-one onto the stream terminal states in plan §4.
package mockupstream

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Scenario names accepted by the handler.
const (
	Complete         = "complete"          // deltas, usage chunk, [DONE]
	CompleteNoUsage  = "complete_no_usage" // deltas, [DONE], no usage
	ErrorBeforeFirst = "error_before"      // HTTP 429 with an error body
	ErrorInStream    = "error_in_stream"   // HTTP 200, first event is an error
	ErrorAfterOutput = "error_after"       // some deltas, then an error event
	EmptyStream      = "empty"             // HTTP 200, stream closes with no events
	Truncated        = "truncated"         // some deltas, connection drops, no [DONE]
	SlowHeaders      = "slow_headers"      // headers only after ttfb_ms (use with a short header timeout)
	InvalidRequest   = "invalid_request"   // HTTP 400: the request is wrong for every upstream
)

// Params controls one mocked response.
type Params struct {
	Scenario string
	Chunks   int
	Interval time.Duration
	TTFB     time.Duration
}

// ParseParams reads Params from a request, with defaults suited to load tests.
func ParseParams(r *http.Request) Params {
	q := r.URL.Query()
	p := Params{Scenario: q.Get("scenario"), Chunks: 20, Interval: 10 * time.Millisecond, TTFB: 50 * time.Millisecond}
	if p.Scenario == "" {
		p.Scenario = Complete
	}
	if v, err := strconv.Atoi(q.Get("chunks")); err == nil && v >= 0 {
		p.Chunks = v
	}
	if v, err := strconv.Atoi(q.Get("interval_ms")); err == nil && v >= 0 {
		p.Interval = time.Duration(v) * time.Millisecond
	}
	if v, err := strconv.Atoi(q.Get("ttfb_ms")); err == nil && v >= 0 {
		p.TTFB = time.Duration(v) * time.Millisecond
	}
	applyPathParams(r.URL.Path, &p)
	return p
}

// applyPathParams reads the optional /m/{scenario}/c{n}/i{ms}/t{ms} prefix.
func applyPathParams(path string, p *Params) {
	rest, ok := strings.CutPrefix(path, "/m/")
	if !ok {
		return
	}
	for i, seg := range strings.Split(rest, "/") {
		if seg == "v1" {
			return
		}
		if i == 0 {
			p.Scenario = seg
			continue
		}
		if len(seg) < 2 {
			continue
		}
		v, err := strconv.Atoi(seg[1:])
		if err != nil || v < 0 {
			continue
		}
		switch seg[0] {
		case 'c':
			p.Chunks = v
		case 'i':
			p.Interval = time.Duration(v) * time.Millisecond
		case 't':
			p.TTFB = time.Duration(v) * time.Millisecond
		}
	}
}

// Handler serves chat completions under any path; see the package comment.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		_ = json.Unmarshal(raw, &body) // unparsable bodies are treated as non-streaming
		p := ParseParams(r)
		if body.Stream {
			serve(w, r, p)
			return
		}
		serveJSON(w, r, p)
	})
}

func serve(w http.ResponseWriter, r *http.Request, p Params) {
	if !sleep(r, p.TTFB) {
		return
	}
	if writeHTTPError(w, p.Scenario) {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	emit := func(data string) bool {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	switch p.Scenario {
	case EmptyStream:
		return
	case ErrorInStream:
		emit(errorEvent)
		return
	}

	chunks := p.Chunks
	if p.Scenario == ErrorAfterOutput || p.Scenario == Truncated {
		chunks = max(1, chunks/2)
	}
	for i := 0; i < chunks; i++ {
		if i > 0 && !sleep(r, p.Interval) {
			return
		}
		if !emit(deltaEvent(i)) {
			return
		}
	}

	switch p.Scenario {
	case ErrorAfterOutput:
		emit(errorEvent)
	case Truncated:
		abort(w)
	case CompleteNoUsage:
		emit("[DONE]")
	default: // Complete, SlowHeaders
		emit(usageEvent(chunks))
		emit("[DONE]")
	}
}

const errorEvent = `{"error":{"message":"upstream overloaded","type":"server_error","code":"overloaded"}}`

// InvalidRequestBody is the exact 400 body, so tests can check pass-through.
const InvalidRequestBody = `{"error":{"message":"messages is required","type":"invalid_request_error","code":"missing_messages"}}`

// writeHTTPError handles the scenarios that fail before any body is streamed.
func writeHTTPError(w http.ResponseWriter, scenario string) bool {
	switch scenario {
	case ErrorBeforeFirst:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error","code":"rate_limited"}}`))
		return true
	case InvalidRequest:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(InvalidRequestBody))
		return true
	}
	return false
}

// serveJSON answers a non-streaming request with one chat.completion object.
func serveJSON(w http.ResponseWriter, r *http.Request, p Params) {
	if !sleep(r, p.TTFB) {
		return
	}
	if writeHTTPError(w, p.Scenario) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if p.Scenario == ErrorInStream || p.Scenario == ErrorAfterOutput {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(errorEvent))
		return
	}
	usage := fmt.Sprintf(`,"usage":{"prompt_tokens":12,"completion_tokens":%d,"total_tokens":%d}`, p.Chunks, 12+p.Chunks)
	if p.Scenario == CompleteNoUsage {
		usage = ""
	}
	_, _ = fmt.Fprintf(w, `{"id":"chatcmpl-mock","object":"chat.completion","model":"mock","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]%s}`,
		strings.Repeat("tok ", p.Chunks), usage)
}

func deltaEvent(i int) string {
	return fmt.Sprintf(`{"id":"chatcmpl-mock","object":"chat.completion.chunk","model":"mock","choices":[{"index":0,"delta":{"content":"tok%d "},"finish_reason":null}]}`, i)
}

func usageEvent(completionTokens int) string {
	return fmt.Sprintf(`{"id":"chatcmpl-mock","object":"chat.completion.chunk","model":"mock","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":%d,"total_tokens":%d}}`,
		completionTokens, 12+completionTokens)
}

// sleep waits d or until the client goes away; it reports whether to continue.
func sleep(r *http.Request, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-r.Context().Done():
		return false
	}
}

// abort drops the connection mid-stream, like an upstream crash.
func abort(w http.ResponseWriter) {
	if hj, ok := w.(http.Hijacker); ok {
		if conn, _, err := hj.Hijack(); err == nil {
			_ = conn.Close()
			return
		}
	}
	panic(http.ErrAbortHandler) // HTTP/2 or recorders: net/http resets the stream
}
