package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Wrap intercepts only Responses background creation. Ordinary request bytes are
// replayed intact, including when background=false or the body is invalid JSON.
func (h *Handler) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || (r.URL.Path != "/v1/responses" && r.URL.Path != "/responses" && r.URL.Path != "/backend-api/codex/responses") {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.cfg.MaxBodyBytes))
		_ = r.Body.Close()
		if err != nil {
			status := http.StatusBadRequest
			var oversized *http.MaxBytesError
			if errors.As(err, &oversized) {
				status = http.StatusRequestEntityTooLarge
			}
			writeError(w, status, "invalid_request", "request body cannot be read")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		if !gjson.ValidBytes(body) || gjson.GetBytes(body, "background").Type != gjson.True {
			next.ServeHTTP(w, r)
			return
		}
		h.createBackgroundJob(w, r, body)
	})
}

func (h *Handler) createBackgroundJob(w http.ResponseWriter, r *http.Request, body []byte) {
	principal, ok := h.authorize(w, r)
	if !ok {
		return
	}
	if !h.backgroundReady() {
		writeError(w, 503, "background_unavailable", "durable background execution is unavailable")
		return
	}
	req, root, ok := h.validateBackgroundCreateRequest(w, r, principal, body)
	if !ok {
		return
	}
	target, ok := h.planBackgroundCreateTarget(w, r, req, root)
	if !ok {
		return
	}
	hold, ok := h.reserveBackgroundCreateBilling(w, r, req, target)
	if !ok {
		return
	}
	job := BackgroundJob{ID: req.ID, UserID: principal.UserID, KeyID: principal.KeyID, Group: principal.Group, Model: req.Model, Path: req.Path,
		ChannelID: target.ChannelID, CredentialID: target.CredentialID, Body: req.Body, ClientIP: h.clientIP(r), PricingHeaders: req.PricingHeaders,
		Reservation: bytes.Clone(hold), Status: "queued", Native: backgroundNative(target.Provider), Stream: req.Stream,
		LastUpstreamSequence: -1, CreatedAt: req.Received, UpdatedAt: req.Received}
	if err := h.cfg.BackgroundJobs.Create(r.Context(), job); err != nil {
		h.refundUncreatedBackground(req, hold)
		writeError(w, 503, "background_store_unavailable", "background request cannot be persisted")
		return
	}
	if job.Stream {
		h.streamBackgroundJob(w, r, job, -1)
		return
	}
	writeBackgroundSnapshot(w, job)
}

// validateBackgroundCreateRequest validates the raw creation body and principal policy, and
// builds the gateway.Request mirror used for planning, billing and the eventual durable job.
func (h *Handler) validateBackgroundCreateRequest(w http.ResponseWriter, r *http.Request, principal gateway.Principal, body []byte) (req *gateway.Request, root gjson.Result, ok bool) {
	root = gjson.ParseBytes(body)
	model := root.Get("model")
	if !root.IsObject() || model.Type != gjson.String || model.Str == "" {
		writeError(w, 400, "invalid_request", "model is required")
		return nil, root, false
	}
	if stream := root.Get("stream"); stream.Exists() && stream.Type != gjson.True && stream.Type != gjson.False {
		writeError(w, 400, "invalid_request", "stream must be a boolean")
		return nil, root, false
	}
	if err := h.validatePolicy(principal, model.Str, r); err != nil {
		writeError(w, 403, "request_not_permitted", err.Error())
		return nil, root, false
	}
	req = &gateway.Request{ID: "resp_bg_" + requestID(), Received: time.Now().UTC(), Protocol: gateway.ProtocolResponses,
		Body: bytes.Clone(body), Model: model.Str, Path: r.URL.Path, Stream: root.Get("stream").Bool(), Principal: principal, PricingHeaders: backgroundPricingHeaders(r.Header)}
	if failure := h.requestGuardFailure(r.Context(), req); failure != nil {
		writeRequestGuardError(w, failure)
		return nil, root, false
	}
	return req, root, true
}

// planBackgroundCreateTarget resolves and pins the single route a new background job will hold:
// it rewrites previous_response_id to the upstream ID when continuing a prior generation,
// restricts planning to that prior route, and checks target policy on the selected target.
func (h *Handler) planBackgroundCreateTarget(w http.ResponseWriter, r *http.Request, req *gateway.Request, root gjson.Result) (target gateway.Target, ok bool) {
	var previousChannel, previousCredential int64
	if id := root.Get("previous_response_id").Str; id != "" {
		upstream, channel, credential, err := h.previousResponse(r.Context(), req, id)
		if err != nil || !backgroundID(upstream) {
			writeError(w, 400, "previous_response_not_found", "previous response is not owned or available")
			return target, false
		}
		previousChannel, previousCredential = channel, credential
		req.Body, err = sjson.SetBytes(req.Body, "previous_response_id", upstream)
		if err != nil {
			writeError(w, 400, "invalid_request", "previous response cannot be prepared")
			return target, false
		}
	}
	targets, err := h.cfg.Planner.Plan(r.Context(), req)
	if err != nil {
		writeError(w, 503, "no_available_channel", "no background route is available")
		return target, false
	}
	if root.Get("previous_response_id").Str != "" {
		filtered := targets[:0]
		for _, candidate := range targets {
			if candidate.ChannelID == previousChannel && candidate.CredentialID == previousCredential {
				filtered = append(filtered, candidate)
			}
		}
		targets = filtered
	}
	if len(targets) == 0 || targets[0].ChannelID <= 0 || targets[0].CredentialID <= 0 {
		writeError(w, 503, "no_available_channel", "no background route is available")
		return target, false
	}
	// A durable hold freezes one selected route, not the remaining retry plan.
	target = targets[0]
	req.Targets = []gateway.Target{target}
	if failure := h.targetPolicyFailure(req, target); failure != nil {
		writeError(w, failure.Status, failure.Code, failure.Message)
		return target, false
	}
	return target, true
}

// reserveBackgroundCreateBilling reserves durable background credits for req against target,
// refunding the hold itself if it comes back invalid (the job was never created, so no other
// refund path would otherwise see it).
func (h *Handler) reserveBackgroundCreateBilling(w http.ResponseWriter, r *http.Request, req *gateway.Request, target gateway.Target) (hold json.RawMessage, ok bool) {
	hold, err := h.cfg.BackgroundBilling.Reserve(r.Context(), req)
	if err != nil {
		status := 503
		if errors.Is(err, gateway.ErrInsufficientCredits) {
			status = 402
		}
		writeError(w, status, "billing_unavailable", "unable to reserve background credits")
		return nil, false
	}
	if !json.Valid(hold) || string(hold) == "null" {
		h.refundUncreatedBackground(req, hold)
		writeError(w, 503, "billing_unavailable", "durable reservation is invalid")
		return nil, false
	}
	return hold, true
}

func backgroundPricingHeaders(headers http.Header) map[string]string {
	result := make(map[string]string)
	for name, values := range headers {
		switch strings.ToLower(name) {
		case "authorization", "x-api-key", "api-key", "x-goog-api-key", "cookie", "proxy-authorization", "sec-websocket-protocol":
			continue
		}
		result[name] = strings.Join(values, ",")
	}
	return result
}

func (h *Handler) refundUncreatedBackground(req *gateway.Request, hold json.RawMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.FinalizeTimeout)
	defer cancel()
	out := gateway.Decide(gateway.Observation{Err: backgroundFailure("background_store_unavailable")})
	if err := h.cfg.BackgroundBilling.Finalize(ctx, req, hold, out); err != nil {
		h.cfg.Logger.Error("uncreated background reservation refund failed", "request_id", req.ID, "err", err)
	}
}

func backgroundNative(provider string) bool {
	return provider == "openai" || provider == "responses" || provider == "openaimax" || provider == "openai_max" || provider == "azure"
}
