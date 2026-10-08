package auxiliary

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

// sseRelay holds the mutable state for a single SSE relay pass. Fields mirror
// what used to be closure-captured locals in relaySSE.
type sseRelay struct {
	h    *Handler
	ctx  context.Context
	w    http.ResponseWriter
	req  *gateway.Request
	in   Input
	resp *http.Response

	event strings.Builder
	data  strings.Builder
	name  string

	delivered   bool
	usage       gateway.Usage
	outputBytes int64
	consumed    bool
	done        bool
	eventErr    *gateway.UpstreamError
	gone        bool
	drainTimer  *time.Timer
	writeFailed bool
	scanErr     error
}

// SSE is relayed event-by-event. No candidate is retried after client output.
func (h *Handler) relaySSE(ctx context.Context, w http.ResponseWriter, req *gateway.Request, in Input, resp *http.Response) (gateway.Outcome, gateway.AttemptResult) {
	r := &sseRelay{
		h:     h,
		ctx:   ctx,
		w:     w,
		req:   req,
		in:    in,
		resp:  resp,
		usage: estimate(req, in, nil),
	}
	defer r.stopDrainTimer()
	r.run()
	return r.finalize()
}

func (r *sseRelay) stopDrainTimer() {
	if r.drainTimer != nil {
		r.drainTimer.Stop()
	}
}

// run scans the upstream SSE body, flushing each event to the client as it
// completes, then records the terminal scanner error (if any).
func (r *sseRelay) run() {
	scanner := bufio.NewScanner(r.resp.Body)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if !r.flush() {
				r.writeFailed = r.eventErr == nil
				break
			}
			continue
		}
		r.event.WriteString(line)
		r.event.WriteByte('\n')
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			r.data.WriteString(strings.TrimPrefix(value, " "))
			r.data.WriteByte('\n')
		}
		if value, ok := strings.CutPrefix(line, "event:"); ok {
			r.name = strings.TrimSpace(value)
		}
	}
	if !r.writeFailed && r.eventErr == nil && r.event.Len() > 0 {
		r.writeFailed = !r.flush() && r.eventErr == nil
	}
	r.scanErr = scanner.Err()
}

// flush parses the buffered event, classifies it, and emits it to the client
// (or drops it) before resetting the buffers for the next event.
func (r *sseRelay) flush() bool {
	payload := strings.TrimSuffix(r.data.String(), "\n")
	if payload == "" && r.name == "" {
		r.event.Reset()
		return true
	}
	root := gjson.Parse(payload)
	isError := r.classifyPayload(payload, root)
	if isError && !r.delivered {
		return false
	}
	if isError {
		r.rewriteAsErrorEvent()
	}
	isOutput := r.classifyOutput(payload, root, isError)
	r.enrichUsageFromPayload(payload)
	return r.emit(isError, isOutput)
}

// classifyPayload updates done/usage state from the parsed event and reports
// whether the event represents an upstream error.
func (r *sseRelay) classifyPayload(payload string, root gjson.Result) bool {
	r.done = r.done || payload == "[DONE]" || root.Get("type").String() == "response.completed"
	invalidUsage := false
	tier := r.usage.ServiceTier
	if reported := responseServiceTier(root); reported != "" {
		tier = reported
	}
	if value := parseUsage([]byte(payload)); value != nil {
		invalidUsage = !validUsage(*value)
		if !invalidUsage {
			r.usage = *value
		}
	}
	r.usage.ServiceTier = tier
	isError := invalidUsage || r.name == "error" || root.Get("type").String() == "error" || root.Get("error").IsObject()
	if isError {
		r.eventErr = failure(502, "upstream_stream_error", "upstream stream reported an error")
	}
	return isError
}

// rewriteAsErrorEvent replaces the buffered event with a sanitized error
// payload once an error has been detected and already partially delivered.
func (r *sseRelay) rewriteAsErrorEvent() {
	safe, _ := json.Marshal(map[string]any{"error": map[string]string{"type": r.eventErr.Type, "code": r.eventErr.Code, "message": r.eventErr.Message}})
	r.event.Reset()
	r.event.WriteString("data: ")
	r.event.Write(safe)
	r.event.WriteByte('\n')
}

// classifyOutput decides whether the event carries user-visible output and
// tracks output byte accounting when it does.
func (r *sseRelay) classifyOutput(payload string, root gjson.Result, isError bool) bool {
	isOutput := payload != "[DONE]" && !isError && r.name != "usage" && root.Get("type").String() != "usage" && payload != ""
	if root.Get("usage").IsObject() && root.Get("type").String() == "" && !root.Get("choices").Exists() && !root.Get("data").Exists() {
		isOutput = false
	}
	if root.Get("choices").Exists() && !streamHasOutput(root) {
		isOutput = false
	}
	if isOutput {
		r.outputBytes += streamTextBytes(root)
		r.consumed = true
	}
	return isOutput
}

func (r *sseRelay) enrichUsageFromPayload(payload string) {
	enrichUsage(&r.usage, r.req, r.in, Response{Body: []byte(payload), Header: r.resp.Header})
}

// emit either drops the buffered event (client gone, or nothing delivered
// yet and no output) or writes it to the client, then resets the buffers.
func (r *sseRelay) emit(isError, isOutput bool) bool {
	r.gone = r.gone || clientGone(r.ctx)
	if r.gone {
		r.event.Reset()
		r.data.Reset()
		r.name = ""
		return !isError
	}
	if !r.delivered && !isOutput {
		r.event.Reset()
		r.data.Reset()
		r.name = ""
		return true
	}
	r.writeEvent(isOutput)
	r.event.Reset()
	r.data.Reset()
	r.name = ""
	return r.eventErr == nil
}

// writeEvent writes the headers (on first delivery) and the buffered event
// to the client, arming the drain timer if the write itself fails.
func (r *sseRelay) writeEvent(isOutput bool) {
	if !r.delivered {
		copyHeaders(r.w.Header(), r.resp.Header)
		r.w.Header().Set("Content-Type", "text/event-stream")
		r.w.WriteHeader(r.resp.StatusCode)
	}
	n, err := io.WriteString(r.w, r.event.String()+"\n")
	if isOutput && n > 0 {
		r.delivered = true
	}
	if err != nil {
		r.gone = true
		if cancel, ok := r.ctx.Value(cancelKey{}).(context.CancelFunc); ok {
			r.drainTimer = time.AfterFunc(r.h.cfg.DrainTimeout, cancel)
		}
	}
	if f, ok := r.w.(http.Flusher); ok {
		f.Flush()
	}
}

// finalize derives the terminal error (if any) and translates the relay
// state into the outcome/attempt-result pair returned by relaySSE.
func (r *sseRelay) finalize() (gateway.Outcome, gateway.AttemptResult) {
	if r.usage.Estimated {
		r.usage.CompletionTokens = (r.outputBytes + 3) / 4
	}
	if r.scanErr != nil && r.eventErr == nil {
		r.eventErr = failure(502, "upstream_stream_cut", "upstream stream ended unexpectedly")
	}
	if !r.done && r.eventErr == nil && r.consumed {
		r.eventErr = failure(502, "upstream_stream_cut", "upstream stream ended unexpectedly")
	}
	if errors.Is(r.ctx.Err(), context.DeadlineExceeded) {
		r.eventErr = failure(504, "upstream_timeout", "upstream response timed out")
	}
	if !r.delivered && !r.consumed && r.eventErr == nil {
		r.eventErr = failure(502, "empty_response", "upstream returned no output")
	}
	out := contextOutcome(r.ctx, r.delivered, r.usage, r.eventErr)
	if r.writeFailed {
		out.Terminal = gateway.TerminalClientCanceled
	}
	if r.gone || clientGone(r.ctx) {
		observation := gateway.Observation{Delivered: r.delivered, ClientCanceled: true, Estimate: r.usage, Err: r.eventErr}
		if r.consumed || !r.usage.Estimated {
			observation.Usage = &r.usage
		}
		out = gateway.Decide(observation)
	}
	return out, gateway.AttemptResult{OK: r.eventErr == nil && !r.writeFailed, Err: r.eventErr, Status: r.resp.StatusCode, Retryable: !r.delivered && r.ctx.Err() == nil && !r.writeFailed && !r.gone && !clientGone(r.ctx), Scope: gateway.ScopeCredential}
}
