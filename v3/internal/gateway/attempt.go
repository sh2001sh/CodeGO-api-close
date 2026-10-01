package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/metrics"
)

const maxErrorBody = 64 << 10

var errDrainTimeout = errors.New("gateway: drain after client disconnect timed out")

// attempt runs one upstream try and streams it to the client. It returns the
// raw observation for decide and the typed result for failover and cooldown.
//
// The upstream context is detached from the client's: when the client leaves,
// the upstream is drained (bounded by DrainTimeout) so its usage is still
// billed, as sub2api does.
func (g *Gateway) attempt(clientCtx context.Context, req *Request, target Target, cs *clientStream) (finish, AttemptResult) {
	if g.targetPolicy != nil && len(req.Attempts) > 0 {
		if err := g.targetPolicy(req, target); err != nil {
			result := requestBuildFailure(err)
			result.Retryable, result.Scope = false, ScopeRequest
			return finish{}, result
		}
	}
	provider, ok := g.providers[target.Provider]
	if !ok {
		return finish{}, AttemptResult{Retryable: true, Scope: ScopeCredential,
			Err: &UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: "provider_not_supported", Message: "channel provider is not supported"}}
	}
	client, err := g.upstreamClient(clientCtx, target)
	if err != nil {
		return finish{}, transportFailure(err)
	}
	if err := g.acquireAttempt(clientCtx, req, target); err != nil {
		return finish{clientGone: clientCtx.Err() != nil}, limitFailure(err)
	}
	defer g.releaseAttempt(req, target)

	upCtx, cancel, cancelCause, headerTimer, stopDrain := g.prepareAttemptContext(clientCtx, req, cs)
	defer cancel()
	defer cancelCause(nil)
	defer stopDrain()

	resp, sent, f, result, sendOK := g.sendProviderRequest(upCtx, provider, req, target, client, headerTimer, cs)
	if !sendOK {
		return f, result
	}

	state := ""
	if target.Provider == "codex" {
		state = resp.Header.Get("X-Codex-Turn-State")
	}
	cs.setCodexState(state)
	events := ApplyResponseSettings(provider.Decode(req, resp), req, target)
	defer func() { _ = events.Close() }()
	f, result = g.relay(upCtx, req, events, cs, sent)
	result.TTFT = f.ttft
	if f.usage != nil && !f.usage.Estimated {
		result.PromptTokens, result.CachedTokens = f.usage.PromptTokens, f.usage.CachedTokens
	}
	return f, result
}

// prepareAttemptContext builds the detached upstream context and its header
// and drain timers. The upstream context outlives the client's: when the
// client leaves, the upstream is drained (bounded by DrainTimeout) so its
// usage is still billed, as sub2api does.
func (g *Gateway) prepareAttemptContext(clientCtx context.Context, req *Request, cs *clientStream) (upCtx context.Context, cancel context.CancelFunc, cancelCause context.CancelCauseFunc, headerTimer *time.Timer, stopDrain func() bool) {
	upCtx, cancel = context.WithTimeoutCause(context.WithoutCancel(clientCtx), g.cfg.RelayTimeout, context.DeadlineExceeded)
	upCtx, cancelCause = context.WithCancelCause(upCtx)

	// Non-streaming upstreams send headers only with the full completion, so
	// only the relay timeout applies to them.
	headerTimeout := g.cfg.HeaderTimeout
	if !req.Stream {
		headerTimeout = g.cfg.RelayTimeout
	}
	headerTimer = time.AfterFunc(headerTimeout, func() { cancelCause(errHeaderTimeout) })
	stopDrain = context.AfterFunc(clientCtx, func() {
		cs.markGone()
		timer := time.NewTimer(g.cfg.DrainTimeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			cancelCause(errDrainTimeout)
		case <-upCtx.Done():
		}
	})
	return upCtx, cancel, cancelCause, headerTimer, stopDrain
}

// sendProviderRequest builds the upstream request, applies overrides, sends
// it, and classifies the response status. ok reports whether resp/sent are
// valid; when ok is false the caller must return f/result as attempt's
// result without further processing.
func (g *Gateway) sendProviderRequest(upCtx context.Context, provider Provider, req *Request, target Target, client *http.Client, headerTimer *time.Timer, cs *clientStream) (resp *http.Response, sent time.Time, f finish, result AttemptResult, ok bool) {
	httpReq, err := BuildProviderRequest(upCtx, provider, req, target)
	if err != nil {
		headerTimer.Stop()
		cause := context.Cause(upCtx)
		if errors.Is(cause, errHeaderTimeout) || errors.Is(cause, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			if errors.Is(cause, errHeaderTimeout) {
				err = errHeaderTimeout
			} else {
				err = context.DeadlineExceeded
			}
			return nil, time.Time{}, finish{clientGone: clientGone(cs), timedOut: true}, transportFailure(err), false
		}
		return nil, time.Time{}, finish{clientGone: clientGone(cs)}, requestBuildFailure(err), false
	}
	for name, value := range req.ClientHeaders {
		if name == "X-Spark-Api-Version" {
			continue // routing input consumed by Spark's signed request builder
		}
		httpReq.Header.Set(name, value)
	}
	if err := ApplyUpstreamRequest(httpReq, req, target); err != nil {
		headerTimer.Stop()
		closeRequestBody(httpReq)
		return nil, time.Time{}, finish{clientGone: clientGone(cs)}, requestBuildFailure(err), false
	}
	if err := FinalizeProviderRequest(upCtx, provider, httpReq, req, target); err != nil {
		headerTimer.Stop()
		closeRequestBody(httpReq)
		return nil, time.Time{}, finish{clientGone: clientGone(cs)}, requestBuildFailure(err), false
	}
	sent = time.Now()
	req.Timeline.MarkUpstreamSent(sent)
	client.Transport = OverrideProviderTransport(provider, req, client.Transport)
	resp, err = client.Do(httpReq)
	headerTimer.Stop()
	if err != nil {
		cause := context.Cause(upCtx)
		if errors.Is(cause, errHeaderTimeout) {
			err = errHeaderTimeout
		}
		f = finish{clientGone: clientGone(cs), timedOut: errors.Is(cause, errHeaderTimeout) || errors.Is(cause, context.DeadlineExceeded)}
		return nil, time.Time{}, f, transportFailure(err), false
	}
	req.Timeline.Mark(metrics.StageUpstreamHeaders, time.Now())
	resp.StatusCode = MapUpstreamStatus(resp.StatusCode, target)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		_ = resp.Body.Close()
		res := classifyStatus(resp.StatusCode, body)
		res.RetryAfter = providerRetryAfter(resp.Header, body, time.Now())
		return nil, time.Time{}, finish{clientGone: clientGone(cs)}, res, false
	}
	return resp, sent, finish{}, AttemptResult{}, true
}

// relay pumps events to the client until the stream ends. Before the first
// data event nothing is visible to the client, so an in-band error or an
// empty stream is reported as retryable and the next candidate takes over.
func (g *Gateway) relay(upCtx context.Context, req *Request, events EventStream, cs *clientStream, sent time.Time) (finish, AttemptResult) {
	var f finish
	var textBytes int64
loop:
	for {
		ev, err := events.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break // upstream closed cleanly without an explicit done marker
			}
			return g.cut(upCtx, req, cs, f, textBytes)
		}
		req.sample.Add(ev)
		switch ev.Kind {
		case EventError:
			return relayError(req, cs, ev, f, textBytes)
		case EventUsage:
			if ev.Usage != nil {
				f.usage = ev.Usage
			}
			textBytes += int64(ev.TextBytes)
		case EventData:
			textBytes += relayData(req, cs, ev, &f, sent)
		case EventDone:
			break loop
		}
	}
	f.delivered, f.clientGone = cs.state()
	f.estimate = estimateUsage(req, textBytes)
	if !f.delivered && !f.clientGone {
		f.empty = true
		return f, streamFailure(&UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error",
			Code: "empty_response", Message: "upstream returned an empty response"})
	}
	return f, AttemptResult{OK: true, Status: http.StatusOK}
}

// relayError handles an EventError from the upstream. Before anything has
// been delivered to the client it is invisible-failover retryable; once
// something was delivered it is terminal and surfaced to the client.
func relayError(req *Request, cs *clientStream, ev Event, f finish, textBytes int64) (finish, AttemptResult) {
	f.delivered, f.clientGone = cs.state()
	f.estimate = estimateUsage(req, textBytes)
	if ev.Usage != nil {
		f.usage = ev.Usage
	}
	if delivered, _ := cs.state(); !delivered {
		f.empty = ev.Err != nil && ev.Err.Code == "empty_response"
		return f, streamFailure(ev.Err) // invisible failover
	}
	f.err = ev.Err
	f.delivered = true
	if len(ev.Payload) > 0 && req.Protocol != ProtocolOpenAIChat {
		cs.writeData(ev.Name, ev.Payload)
	} else {
		cs.fail(ev.Err)
	}
	f.estimate = estimateUsage(req, textBytes)
	return f, AttemptResult{Status: http.StatusOK, Err: ev.Err}
}

// relayData writes an EventData payload to the client, updating f's
// time-to-first-token and usage in place, and returns the event's text
// byte count for the caller to accumulate.
func relayData(req *Request, cs *clientStream, ev Event, f *finish, sent time.Time) int64 {
	if f.ttft == 0 {
		f.ttft = time.Since(sent)
	}
	if ev.Usage != nil {
		f.usage = ev.Usage
	}
	wasDelivered, _ := cs.state()
	if cs.writeData(ev.Name, ev.Payload) && !wasDelivered {
		req.Timeline.Mark(metrics.StageFirstEvent, time.Now())
	}
	return int64(ev.TextBytes)
}

// cut handles a stream that ended with a read error.
func (g *Gateway) cut(upCtx context.Context, req *Request, cs *clientStream, f finish, textBytes int64) (finish, AttemptResult) {
	f.delivered, f.clientGone = cs.state()
	f.estimate = estimateUsage(req, textBytes)
	cause := context.Cause(upCtx)
	switch {
	case f.clientGone:
		return f, AttemptResult{Status: http.StatusOK} // usage so far is whatever was drained
	case errors.Is(cause, context.DeadlineExceeded):
		f.timedOut = true
		f.err = &UpstreamError{Status: http.StatusGatewayTimeout, Type: "upstream_error", Code: "upstream_timeout", Message: "upstream response timed out"}
		if f.delivered {
			cs.fail(f.err)
		}
		return f, AttemptResult{Retryable: !f.delivered, Scope: ScopeCredential, Status: http.StatusOK, Err: f.err}
	default:
		f.err = &UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: "upstream_stream_cut", Message: "upstream stream ended unexpectedly"}
		if !f.delivered {
			return f, streamFailure(f.err)
		}
		cs.fail(f.err)
		return f, AttemptResult{Status: http.StatusOK, Err: f.err}
	}
}

func clientGone(cs *clientStream) bool {
	_, gone := cs.state()
	return gone
}
