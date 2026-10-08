package live

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

func (h *Handler) responseTurn(ctx context.Context, conn *websocket.Conn, req *gateway.Request) (string, []byte, bool, gateway.Outcome) {
	out := gateway.Decide(gateway.Observation{Err: &gateway.UpstreamError{Status: 503, Code: "upstream_unavailable", Message: "no upstream is available"}})
	for index, target := range req.Targets {
		out.Target = &target
		if index > 0 {
			if denied := h.targetPolicyFailure(req, target); denied != nil {
				out = gateway.Decide(gateway.Observation{Err: denied})
				out.Target = &target
				_ = socketError(conn, denied.Status, denied.Code, denied.Message)
				return "", nil, false, out
			}
		}
		provider := h.cfg.Providers[target.Provider]
		if provider == nil {
			continue
		}
		if h.cfg.Limits != nil {
			if err := h.cfg.Limits.Acquire(ctx, req, target); err != nil {
				status := 503
				if errors.Is(err, gateway.ErrRateLimited) || errors.Is(err, gateway.ErrTargetBusy) {
					status = 429
				}
				out.Err = &gateway.UpstreamError{Status: status, Code: "rate_limited", Message: "session request limit reached"}
				if errors.Is(err, gateway.ErrTargetBusy) {
					continue
				}
				break
			}
		}
		id, output, complete, result, attempt := h.responseAttempt(ctx, conn, req, target, provider)
		if h.cfg.Limits != nil {
			releaseCtx, cancel := context.WithTimeout(context.Background(), h.cfg.FinalizeTimeout)
			if err := h.cfg.Limits.Release(releaseCtx, req, target); err != nil {
				h.cfg.Logger.Error("live lease release failed", "request_id", req.ID, "err", err)
			}
			cancel()
		}
		h.cfg.Planner.Report(target, attempt)
		req.Attempts = append(req.Attempts, gateway.Attempt{Target: target, Result: attempt})
		out = result
		if complete || result.Delivered || !attempt.Retryable {
			if !complete && result.Terminal != gateway.TerminalClientCanceled {
				_ = socketError(conn, 502, "upstream_error", "upstream response failed")
			}
			return id, output, complete, out
		}
	}
	if out.Err != nil {
		_ = socketError(conn, out.Err.Status, out.Err.Code, out.Err.Message)
	} else {
		_ = socketError(conn, 503, "upstream_unavailable", "no upstream accepted the response")
	}
	return "", nil, false, out
}

func (h *Handler) responseAttempt(ctx context.Context, conn *websocket.Conn, req *gateway.Request, target gateway.Target, provider gateway.Provider) (string, []byte, bool, gateway.Outcome, gateway.AttemptResult) {
	observed := gateway.Observation{Err: &gateway.UpstreamError{Status: 502, Code: "upstream_unavailable", Message: "upstream request failed"}, Estimate: gateway.Usage{PromptTokens: (int64(len(req.Body)) + 3) / 4, Estimated: true}}
	finish := func() gateway.Outcome { result := gateway.Decide(observed); result.Target = &target; return result }
	attempt := gateway.AttemptResult{Retryable: true, Scope: gateway.ScopeCredential}
	resp, ok := h.sendResponseUpstreamRequest(ctx, req, target, provider, &observed, &attempt)
	if !ok {
		return "", nil, false, finish(), attempt
	}
	stream := provider.Decode(req, resp)
	defer func() { _ = stream.Close() }()
	observed.Err = nil
	return h.readResponseStream(ctx, conn, req, target, stream, &observed, &attempt, finish)
}

// sendResponseUpstreamRequest builds, sends and validates the upstream HTTP response for a
// single attempt. It mutates observed/attempt in place to reflect any failure and returns
// ok=false when the caller must abandon the attempt without reading a stream.
func (h *Handler) sendResponseUpstreamRequest(ctx context.Context, req *gateway.Request, target gateway.Target, provider gateway.Provider, observed *gateway.Observation, attempt *gateway.AttemptResult) (*http.Response, bool) {
	up, err := gateway.BuildProviderRequest(ctx, provider, req, target)
	if err != nil {
		var failure *gateway.UpstreamError
		if errors.As(err, &failure) {
			observed.Err = failure
			attempt.Err = failure
			attempt.Retryable = failure.Status >= 500
		}
		return nil, false
	}
	for name, value := range req.ClientHeaders {
		up.Header.Set(name, value)
	}
	if err := gateway.ApplyUpstreamRequest(up, req, target); err != nil {
		_ = up.Body.Close()
		attempt.Retryable = false
		return nil, false
	}
	if err := gateway.FinalizeProviderRequest(ctx, provider, up, req, target); err != nil {
		_ = up.Body.Close()
		attempt.Retryable = false
		return nil, false
	}
	client, err := h.upstreamClient(ctx, target)
	if err != nil {
		_ = up.Body.Close()
		return nil, false
	}
	client.Transport = gateway.OverrideProviderTransport(provider, req, client.Transport)
	resp, err := client.Do(up)
	if err != nil {
		if ctx.Err() != nil {
			observed.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
			observed.ClientCanceled = errors.Is(ctx.Err(), context.Canceled)
			attempt.Retryable = false
		}
		return nil, false
	}
	resp.StatusCode = gateway.MapUpstreamStatus(resp.StatusCode, target)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		attempt.Status = resp.StatusCode
		attempt.Retryable = resp.StatusCode == 429 || resp.StatusCode >= 500 || resp.StatusCode == 401 || resp.StatusCode == 403
		return nil, false
	}
	return resp, true
}

// readResponseStream drains a decoded upstream event stream, relaying payloads to the client
// socket and tracking usage/delivery in observed/attempt, until a terminal event, stream error
// or client-write failure ends the attempt.
func (h *Handler) readResponseStream(ctx context.Context, conn *websocket.Conn, req *gateway.Request, target gateway.Target, stream gateway.EventStream, observed *gateway.Observation, attempt *gateway.AttemptResult, finish func() gateway.Outcome) (string, []byte, bool, gateway.Outcome, gateway.AttemptResult) {
	var id string
	var output []byte
	var generated int64
	for {
		event, readErr := stream.Next()
		if readErr != nil {
			h.classifyResponseStreamReadError(ctx, readErr, observed, attempt)
			break
		}
		if event.Usage != nil {
			usage := *event.Usage
			observed.Usage = &usage
		}
		if event.ServiceTier != "" {
			observed.ServiceTier = event.ServiceTier
		}
		if event.Kind == gateway.EventError {
			observed.Err = event.Err
			if observed.Err == nil {
				observed.Err = &gateway.UpstreamError{Status: 502, Code: "upstream_error", Message: "upstream response failed"}
			}
			if observed.Delivered {
				attempt.Retryable = false
			}
			break
		}
		if event.Kind == gateway.EventDone {
			attempt.OK = true
			attempt.Retryable = false
			out := finish()
			return id, output, out.Terminal == gateway.TerminalCompleted || out.Terminal == gateway.TerminalCompletedNoUsage, out, *attempt
		}
		if len(event.Payload) == 0 {
			continue
		}
		var relayed bool
		id, output, relayed = h.relayResponseStreamEvent(ctx, conn, req, target, event, id, output, observed, attempt)
		if !relayed {
			break
		}
		generated += int64(event.TextBytes)
		observed.Estimate.CompletionTokens = (generated + 3) / 4
	}
	return id, output, false, finish(), *attempt
}

// classifyResponseStreamReadError records observed/attempt state for a stream.Next() failure,
// distinguishing a clean EOF (possibly after partial delivery) from context cancellation/timeout
// and from any other interruption.
func (h *Handler) classifyResponseStreamReadError(ctx context.Context, readErr error, observed *gateway.Observation, attempt *gateway.AttemptResult) {
	if errors.Is(readErr, io.EOF) {
		observed.Empty = !observed.Delivered
		if observed.Delivered {
			observed.Err = &gateway.UpstreamError{Status: 502, Code: "stream_interrupted", Message: "upstream stream ended without a terminal event"}
		}
		attempt.Retryable = !observed.Delivered
		return
	}
	if ctx.Err() != nil {
		observed.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
		observed.ClientCanceled = errors.Is(ctx.Err(), context.Canceled)
		attempt.Retryable = false
		return
	}
	observed.Err = &gateway.UpstreamError{Status: 502, Code: "stream_interrupted", Message: "upstream stream was interrupted"}
	attempt.Retryable = !observed.Delivered
}

// relayResponseStreamEvent persists a newly observed response ID, captures the final output
// payload, and forwards the event to the client socket. relayed is false if persistence or the
// client write fails, telling the caller to stop reading the stream.
func (h *Handler) relayResponseStreamEvent(ctx context.Context, conn *websocket.Conn, req *gateway.Request, target gateway.Target, event gateway.Event, id string, output []byte, observed *gateway.Observation, attempt *gateway.AttemptResult) (newID string, newOutput []byte, relayed bool) {
	root := gjson.ParseBytes(event.Payload)
	if next := root.Get("response.id").Str; next != "" && next != id {
		if err := h.remember(ctx, req, target, next); err != nil {
			observed.Err = &gateway.UpstreamError{Status: 503, Code: "locator_unavailable", Message: "response persistence is unavailable"}
			attempt.Retryable = false
			return id, output, false
		}
		id = next
	}
	if root.Get("type").Str == "response.completed" || root.Get("type").Str == "response.incomplete" {
		if value := root.Get("response.output"); value.Exists() {
			output = []byte(value.Raw)
		}
	}
	if err := frameCodec.Send(conn, wireFrame{data: event.Payload, kind: websocket.TextFrame}); err != nil {
		observed.ClientCanceled = true
		attempt.Retryable = false
		return id, output, false
	}
	observed.Delivered = true
	return id, output, true
}
