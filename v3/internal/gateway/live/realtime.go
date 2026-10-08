package live

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

func (h *Handler) serveRealtime(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r)
	if !ok {
		return
	}
	model := strings.TrimSpace(r.URL.Query().Get("model"))
	if model == "" {
		writeError(w, 400, "model_required", "model is required")
		return
	}
	if err := h.validatePolicy(p, model, r); err != nil {
		writeError(w, 403, "request_not_permitted", err.Error())
		return
	}
	if !isWebSocket(r) {
		w.Header().Set("Upgrade", "websocket")
		writeError(w, 426, "websocket_required", "WebSocket upgrade is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.cfg.SessionTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"model": model, "stream": true})
	req := &gateway.Request{ID: requestID(), Received: time.Now(), Protocol: gateway.ProtocolResponses, Body: body, Model: model, Path: r.URL.Path, Stream: true, Principal: p, PricingHeaders: backgroundPricingHeaders(r.Header)}
	if failure := h.requestGuardFailure(ctx, req); failure != nil {
		writeRequestGuardError(w, failure)
		return
	}
	targets, err := h.cfg.Planner.Plan(ctx, req)
	if err != nil || len(targets) == 0 {
		writeError(w, 503, "no_available_channel", "no realtime channel available")
		return
	}
	req.Targets = targets
	if !h.reserve(w, r.WithContext(ctx), req) {
		return
	}
	out := gateway.Decide(gateway.Observation{Err: &gateway.UpstreamError{Status: 503, Code: "upstream_unavailable", Message: "upstream realtime connection failed"}})
	finalized := false
	defer func() {
		if !finalized {
			h.finalize(req, out)
		}
	}()
	if h.connectRealtimeUpstream(ctx, w, r, req, targets, model, &out, &finalized) {
		return
	}
	writeError(w, 503, "upstream_unavailable", "no upstream accepted the realtime session")
}

// connectRealtimeUpstream tries planned targets and bridges the first accepted
// connection. A true result means the caller must not write another response.
func (h *Handler) connectRealtimeUpstream(ctx context.Context, w http.ResponseWriter, r *http.Request, req *gateway.Request, targets []gateway.Target, model string, out *gateway.Outcome, finalized *bool) (ok bool) {
	for _, target := range targets {
		cfg, err := realtimeConfig(target, model)
		if err != nil {
			continue
		}
		if h.cfg.Limits != nil {
			if err := h.cfg.Limits.Acquire(ctx, req, target); err != nil {
				writeError(w, 429, "rate_limited", "realtime session limit reached")
				return true
			}
		}
		started := time.Now()
		out.Target = &target
		upstream, err := h.connectRealtime(ctx, cfg, req, target)
		if err != nil {
			if h.cfg.Limits != nil {
				h.release(req, target)
			}
			result := gateway.AttemptResult{Retryable: true, Scope: gateway.ScopeCredential}
			req.Attempts = append(req.Attempts, gateway.Attempt{Target: target, Result: result, Duration: time.Since(started)})
			h.cfg.Planner.Report(target, result)
			continue
		}
		defer func() { _ = upstream.Close() }()
		if h.cfg.Limits != nil {
			defer h.release(req, target)
		}
		out.Target = &target
		req.Attempts = append(req.Attempts, gateway.Attempt{Target: target, Result: gateway.AttemptResult{OK: true, Status: 101}, Duration: time.Since(started)})
		h.cfg.Planner.Report(target, gateway.AttemptResult{OK: true, Status: 101})
		acceptWebSocket(w, r, func(client *websocket.Conn) {
			*finalized = true
			*out = h.bridgeRealtime(ctx, client, upstream, r, req, target)
		})
		return true
	}
	return false
}

func (h *Handler) release(req *gateway.Request, target gateway.Target) {
	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.FinalizeTimeout)
	defer cancel()
	if err := h.cfg.Limits.Release(ctx, req, target); err != nil {
		h.cfg.Logger.Error("live lease release failed", "request_id", req.ID, "err", err)
	}
}

func realtimeConfig(target gateway.Target, model string) (*websocket.Config, error) {
	if target.UpstreamModel != "" {
		model = target.UpstreamModel
	}
	u, err := url.Parse(target.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("live: invalid upstream URL")
	}
	if u.User != nil {
		return nil, errors.New("live: URL credentials are forbidden")
	}
	if err := applyRealtimeProviderPath(u, target, model); err != nil {
		return nil, err
	}
	originScheme := u.Scheme
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	cfg, err := websocket.NewConfig(u.String(), originScheme+"://"+u.Host)
	if err != nil {
		return nil, err
	}
	cfg.TlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
	if target.Provider == "azure" {
		cfg.Header.Set("Api-Key", target.Secret)
	} else {
		cfg.Header.Set("Authorization", "Bearer "+target.Secret)
	}
	cfg.Header.Set("OpenAI-Beta", "realtime=v1")
	if target.Fingerprint.UserAgent != "" {
		cfg.Header.Set("User-Agent", target.Fingerprint.UserAgent)
	}
	for name, value := range target.HeaderOverride {
		cfg.Header.Set(name, value)
	}
	return cfg, nil
}

// applyRealtimeProviderPath rewrites u in place to the provider-specific realtime endpoint path
// and query parameters (Azure deployment/api-version, or OpenAI-compatible model query),
// rejecting providers that don't support realtime at all.
func applyRealtimeProviderPath(u *url.URL, target gateway.Target, model string) error {
	base := strings.TrimRight(u.Path, "/")
	q := u.Query()
	if target.Provider == "azure" {
		base = strings.TrimSuffix(base, "/v1")
		if !strings.HasSuffix(base, "/openai") {
			base += "/openai"
		}
		u.Path = base + "/realtime"
		if q.Get("api-version") == "" {
			version, _ := target.Settings["api_version"].(string)
			if strings.TrimSpace(version) == "" {
				version = "2025-04-01-preview"
			}
			q.Set("api-version", version)
		}
		q.Set("deployment", model)
	} else {
		switch target.Provider {
		case "openai", "custom", "openai_max", "openaimax", "openai-max", "xai":
		default:
			return errors.New("live: provider does not support realtime")
		}
		base = strings.TrimSuffix(base, "/responses")
		if !strings.HasSuffix(base, "/v1") {
			base += "/v1"
		}
		u.Path = base + "/realtime"
		q.Set("model", model)
	}
	u.RawPath = ""
	u.RawQuery = q.Encode()
	return nil
}

func (h *Handler) bridgeRealtime(ctx context.Context, client, upstream *websocket.Conn, r *http.Request, req *gateway.Request, target gateway.Target) gateway.Outcome {
	defer func() { _ = client.Close() }()
	defer func() { _ = upstream.Close() }()
	deadline, _ := ctx.Deadline()
	_ = client.SetDeadline(deadline)
	_ = upstream.SetDeadline(deadline)
	client.MaxPayloadBytes = int(h.cfg.MaxBodyBytes)
	upstream.MaxPayloadBytes = int(h.cfg.MaxBodyBytes)
	stop := context.AfterFunc(ctx, func() { _ = client.Close(); _ = upstream.Close() })
	defer stop()
	ends := make(chan relayEnd, 2)
	accounting := &realtimeAccounting{req: req, out: gateway.Outcome{Target: &target}, seen: map[string]bool{}}
	go func() {
		ends <- h.forwardRealtime(ctx, client, upstream, r, req, target, accounting)
	}()
	go func() {
		ends <- h.relayRealtimeUpstream(ctx, client, upstream, r, target, accounting)
	}()
	end := <-ends
	_ = client.Close()
	_ = upstream.Close()
	<-ends // join both loops before reading their accounting observations
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	if accounting.req == nil {
		return accounting.out
	}
	observed := gateway.Observation{Delivered: accounting.out.Delivered, TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded), ClientCanceled: end.client,
		Estimate: gateway.Usage{PromptTokens: (int64(len(accounting.req.Body)) + 3) / 4, CompletionTokens: (accounting.outputChars + 3) / 4, Estimated: true}}
	if accounting.usageReported {
		observed.Usage = &accounting.out.Usage
	}
	if end.err != nil && !errors.Is(end.err, io.EOF) && !end.client && ctx.Err() == nil {
		observed.Err = &gateway.UpstreamError{Status: 502, Code: "realtime_interrupted", Message: "upstream realtime connection was interrupted"}
	}
	result := gateway.Decide(observed)
	result.Target = accounting.out.Target
	h.finalize(accounting.req, result)
	return result
}

// relayRealtimeUpstream reads frames from upstream, accounts for usage/delivery/output size,
// and forwards each frame to client, until a receive or send error ends the relay.
func (h *Handler) relayRealtimeUpstream(ctx context.Context, client, upstream *websocket.Conn, r *http.Request, target gateway.Target, accounting *realtimeAccounting) relayEnd {
	for {
		var f wireFrame
		if err := frameCodec.Receive(upstream, &f); err != nil {
			return relayEnd{err: err}
		}
		root := gjson.ParseBytes(f.data)
		accounting.mu.Lock()
		typ := root.Get("type").Str
		if typ == "response.created" {
			accounting.pending = true
		}
		if typ == "response.done" {
			response := root.Get("response")
			id := response.Get("id").Str
			usage := response.Get("usage")
			if id == "" || !accounting.seen[id] {
				accounting.seen[id] = true
				if usage.IsObject() {
					addRealtimeUsage(&accounting.out.Usage, usage)
					accounting.usageReported = true
				}
				accounting.out.Delivered = accounting.out.Delivered || response.Get("output.#").Int() > 0
				err := h.completeRealtimeTurn(ctx, r, target, accounting)
				if err != nil {
					accounting.mu.Unlock()
					// Stop upstream consumption immediately, even if the client
					// has stopped reading and terminal delivery blocks.
					_ = upstream.Close()
					// Preserve the accepted terminal response before reporting why
					// the next turn cannot be admitted.
					_ = frameCodec.Send(client, f)
					status := 503
					if errors.Is(err, gateway.ErrInsufficientCredits) {
						status = 402
					}
					if errors.Is(err, gateway.ErrInvalidKey) {
						status = 401
					}
					var failure *gateway.UpstreamError
					if errors.As(err, &failure) {
						status = failure.Status
					}
					_ = socketError(client, status, "realtime_admission_failed", "unable to authorize or reserve the next realtime turn")
					return relayEnd{err: err}
				}
			}
		}
		if text := root.Get("delta"); text.Type == gjson.String && (strings.Contains(root.Get("type").Str, "text") || strings.Contains(root.Get("type").Str, "transcript")) {
			accounting.outputChars += int64(len(text.Str))
		}
		accounting.mu.Unlock()
		if err := frameCodec.Send(client, f); err != nil {
			return relayEnd{client: true, err: err}
		}
		accounting.mu.Lock()
		if strings.HasSuffix(typ, ".delta") && root.Get("delta").Str != "" {
			accounting.out.Delivered = true
		}
		accounting.mu.Unlock()
	}
}
