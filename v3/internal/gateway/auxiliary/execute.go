package auxiliary

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func (h *Handler) execute(w http.ResponseWriter, client *http.Request, req *gateway.Request, in Input) gateway.Outcome {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(client.Context()), h.cfg.RelayTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, clientContextKey{}, client.Context())
	ctx = context.WithValue(ctx, cancelKey{}, context.CancelFunc(cancel))
	stop := context.AfterFunc(client.Context(), func() {
		timer := time.NewTimer(h.cfg.DrainTimeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			cancel()
		case <-ctx.Done():
		}
	})
	defer stop()
	var out gateway.Outcome
	for i := range req.Targets {
		target := req.Targets[i]
		started := time.Now()
		if i > 0 {
			if denied := h.targetPolicyFailure(req, target); denied != nil {
				out = contextOutcome(ctx, false, gateway.Usage{}, denied)
				out.Target = &req.Targets[i]
				req.Attempts = append(req.Attempts, gateway.Attempt{Target: target, Result: gateway.AttemptResult{Err: denied, Status: denied.Status, Scope: gateway.ScopeRequest}, Duration: time.Since(started)})
				break
			}
		}
		var result gateway.AttemptResult
		out, result = h.attempt(ctx, w, req, target, in)
		out.Target = &req.Targets[i]
		req.Attempts = append(req.Attempts, gateway.Attempt{Target: target, Result: result, Duration: time.Since(started)})
		h.cfg.Planner.Report(target, result)
		if result.OK || out.Delivered || !result.Retryable || ctx.Err() != nil || client.Context().Err() != nil {
			break
		}
	}
	if client.Context().Err() != nil {
		observation := gateway.Observation{Delivered: out.Delivered, ClientCanceled: true, Estimate: out.Usage, Err: out.Err}
		if out.Charge {
			observation.Usage = &out.Usage
		}
		target := out.Target
		out = gateway.Decide(observation)
		out.Target = target
	}
	if !out.Delivered && out.Err != nil && client.Context().Err() == nil {
		writeError(w, out.Err)
	}
	return out
}

func (h *Handler) attempt(ctx context.Context, w http.ResponseWriter, req *gateway.Request, target gateway.Target, in Input) (gateway.Outcome, gateway.AttemptResult) {
	adapter := h.adapters[target.Provider]
	if adapter == nil {
		err := failure(502, "unsupported_operation", "channel does not support endpoint")
		return contextOutcome(ctx, false, gateway.Usage{}, err), gateway.AttemptResult{Err: err, Retryable: true, Scope: gateway.ScopeModel}
	}
	if h.cfg.Limits != nil {
		if err := h.cfg.Limits.Acquire(ctx, req, target); err != nil {
			return admissionFailure(ctx, err)
		}
		defer h.release(req, target)
	}
	channelClient, err := h.channelClient(ctx, target)
	if err != nil {
		return attemptFailure(ctx, err)
	}
	ctx = context.WithValue(ctx, clientKey{}, channelClient)
	resp, outcome, result, done := h.sendUpstreamRequest(ctx, req, target, in, adapter, channelClient)
	if done {
		return outcome, result
	}
	defer func() { _ = resp.Body.Close() }()
	return h.decodeUpstreamResponse(ctx, w, req, target, in, adapter, resp)
}

// admissionFailure maps a limiter admission error to the outcome/result pair
// attempt returns when a target cannot be leased.
func admissionFailure(ctx context.Context, err error) (gateway.Outcome, gateway.AttemptResult) {
	code, retry, scope := "limits_unavailable", false, gateway.ScopeNone
	status := 503
	switch {
	case errors.Is(err, gateway.ErrRateLimited):
		status, code = 429, "rate_limited"
	case errors.Is(err, gateway.ErrTargetBusy):
		status, code, retry, scope = 429, "target_busy", true, gateway.ScopeModel
	}
	upstream := failure(status, code, "request admission temporarily unavailable")
	return contextOutcome(ctx, false, gateway.Usage{}, upstream), gateway.AttemptResult{Err: upstream, Retryable: retry, Scope: scope}
}

// buildFailure maps an adapter Build/sign error to the outcome/result pair
// attempt returns, honoring intentional param-override returns and upstream
// client errors before falling back to the generic attempt failure.
func buildFailure(ctx context.Context, err error) (gateway.Outcome, gateway.AttemptResult) {
	var intentional *gateway.ParamOverrideReturnError
	if errors.As(err, &intentional) {
		return contextOutcome(ctx, false, gateway.Usage{}, intentional.Upstream), gateway.AttemptResult{Err: intentional.Upstream, Status: intentional.Upstream.Status, Scope: gateway.ScopeRequest, Retryable: !intentional.SkipRetry}
	}
	var clientErr *gateway.UpstreamError
	if errors.As(err, &clientErr) {
		if clientErr.Status >= 500 && clientErr.Code != "unsupported_operation" {
			return attemptFailure(ctx, err)
		}
		result := gateway.AttemptResult{Err: clientErr, Scope: gateway.ScopeRequest}
		if clientErr.Code == "unsupported_operation" {
			result.Retryable, result.Scope = true, gateway.ScopeModel
		}
		return contextOutcome(ctx, false, gateway.Usage{}, clientErr), result
	}
	return attemptFailure(ctx, err)
}

// sendUpstreamRequest builds the upstream request, sends it, and maps
// non-2xx upstream responses to a terminal outcome. When done is true the
// caller must return outcome/result as-is; otherwise resp is a live 2xx
// response the caller now owns (and must close).
func (h *Handler) sendUpstreamRequest(ctx context.Context, req *gateway.Request, target gateway.Target, in Input, adapter Adapter, channelClient *http.Client) (resp *http.Response, outcome gateway.Outcome, result gateway.AttemptResult, done bool) {
	upstream, err := adapter.Build(ctx, req, target, in)
	if err == nil {
		err = gateway.ApplyUpstreamRequest(upstream, req, target)
	}
	if err == nil && target.Provider == "jimeng" {
		err = signJimengMedia(upstream, target.Secret, time.Now().UTC())
	}
	if err != nil {
		outcome, result = buildFailure(ctx, err)
		return nil, outcome, result, true
	}
	if upstream.URL.Scheme == "ws" || upstream.URL.Scheme == "wss" {
		resp, err = mediaWebsocketRoundTrip(ctx, upstream)
	} else {
		client := *upstreamClient(ctx)
		if selector, ok := adapter.(gateway.TransportProvider); ok {
			client.Transport = selector.UpstreamTransport(req, channelClient.Transport)
		}
		resp, err = client.Do(upstream)
	}
	if err != nil {
		outcome, result = attemptFailure(ctx, err)
		return nil, outcome, result, true
	}
	resp.StatusCode = gateway.MapUpstreamStatus(resp.StatusCode, target)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		result = classify(resp.StatusCode)
		result.RetryAfter = retryAfter(resp.Header)
		return nil, contextOutcome(ctx, false, gateway.Usage{}, result.Err), result, true
	}
	resp.Body = http.MaxBytesReader(nil, resp.Body, h.cfg.MaxResponseBytes)
	return resp, gateway.Outcome{}, gateway.AttemptResult{}, false
}

// decodeUpstreamResponse converts a successful (2xx) upstream response into
// the client-facing outcome, relaying as SSE when applicable or decoding and
// writing a single buffered response otherwise.
func (h *Handler) decodeUpstreamResponse(ctx context.Context, w http.ResponseWriter, req *gateway.Request, target gateway.Target, in Input, adapter Adapter, resp *http.Response) (gateway.Outcome, gateway.AttemptResult) {
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		if converter, ok := adapter.(streamAdapter); ok {
			if events := converter.DecodeStream(req, target, in, resp); events != nil {
				resp.Body = &eventReader{events: events}
			}
		}
		return h.relaySSE(ctx, w, req, in, resp)
	}
	response, err := adapter.Decode(ctx, req, target, in, resp)
	if err != nil {
		return attemptFailure(ctx, err)
	}
	if int64(len(response.Body)) > h.cfg.MaxResponseBytes {
		return attemptFailure(ctx, errors.New("converted response too large"))
	}
	if len(response.Body) == 0 {
		empty := failure(502, "empty_response", "upstream returned an empty response")
		return contextOutcome(ctx, false, gateway.Usage{}, empty), gateway.AttemptResult{Err: empty, Retryable: true, Scope: gateway.ScopeCredential}
	}
	usage := estimate(req, in, response.Body)
	if response.Usage != nil {
		if !validUsage(*response.Usage) {
			return attemptFailure(ctx, errors.New("invalid upstream usage"))
		}
		usage = *response.Usage
	}
	enrichUsage(&usage, req, in, response)
	if ctx.Err() != nil {
		return attemptFailure(ctx, ctx.Err())
	}
	if clientGone(ctx) {
		return gateway.Decide(gateway.Observation{ClientCanceled: true, Usage: &usage, Estimate: usage}), gateway.AttemptResult{OK: true, Status: resp.StatusCode}
	}
	return writeDecodedResponse(ctx, w, resp, usage, response)
}

// writeDecodedResponse writes a fully-decoded, non-streaming response to the
// client and derives the final outcome from whether the write landed.
func writeDecodedResponse(ctx context.Context, w http.ResponseWriter, resp *http.Response, usage gateway.Usage, response Response) (gateway.Outcome, gateway.AttemptResult) {
	copyHeaders(w.Header(), response.Header)
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	n, writeErr := w.Write(response.Body)
	out := contextOutcome(ctx, n > 0, usage, nil)
	if writeErr != nil {
		out = gateway.Decide(gateway.Observation{Delivered: n > 0, ClientCanceled: true, Usage: &usage, Estimate: usage})
	}
	return out, gateway.AttemptResult{OK: true, Status: resp.StatusCode}
}

func attemptFailure(ctx context.Context, cause error) (gateway.Outcome, gateway.AttemptResult) {
	err := failure(502, "upstream_failure", "upstream request failed")
	var reported *gateway.UpstreamError
	if errors.As(cause, &reported) && reported.Status >= 400 && reported.Status < 500 && ctx.Err() == nil {
		return contextOutcome(ctx, false, gateway.Usage{}, reported), gateway.AttemptResult{Err: reported, Scope: gateway.ScopeRequest, Status: reported.Status}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(cause, context.DeadlineExceeded) {
		err = failure(504, "upstream_timeout", "upstream request timed out")
	}
	out := contextOutcome(ctx, false, gateway.Usage{}, err)
	return out, gateway.AttemptResult{Err: err, Retryable: ctx.Err() == nil, Scope: gateway.ScopeCredential}
}

func (h *Handler) release(req *gateway.Request, target gateway.Target) {
	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.FinalizeTimeout)
	defer cancel()
	if err := h.cfg.Limits.Release(ctx, req, target); err != nil {
		h.cfg.Logger.Error("auxiliary lease release failed", "request_id", req.ID, "err", err)
	}
}

type clientKey struct{}
type clientContextKey struct{}
type cancelKey struct{}

func clientGone(ctx context.Context) bool {
	client, ok := ctx.Value(clientContextKey{}).(context.Context)
	return ok && client.Err() != nil
}

func upstreamClient(ctx context.Context) *http.Client {
	if client, ok := ctx.Value(clientKey{}).(*http.Client); ok {
		return client
	}
	return &http.Client{CheckRedirect: noRedirect}
}

func noRedirect(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
