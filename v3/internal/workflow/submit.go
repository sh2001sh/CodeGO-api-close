package workflow

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r)
	if !ok {
		return
	}
	in, body, origin, ok := h.prepareSubmit(w, r, p)
	if !ok {
		return
	}
	req, target, adapter, ok := h.selectTarget(w, r, in, body, p, origin)
	if !ok {
		return
	}
	release, ok := h.acquireLease(w, r, req, target)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}
	reservation, ok := h.reserveCredits(w, r, req)
	if !ok {
		return
	}
	t, ok := h.createTask(w, req, target, in, body, p, reservation)
	if !ok {
		return
	}
	result, submitErr := h.runSubmit(req, target, adapter, in, &t)
	h.finishSubmit(w, r, &t, result, submitErr, target)
}

// prepareSubmit parses the request body, resolves a remix origin task if the
// path requests one, and enforces the model/policy gates. It writes the
// failure response itself when ok is false.
func (h *Handler) prepareSubmit(w http.ResponseWriter, r *http.Request, p gateway.Principal) (native.Submit, []byte, Task, bool) {
	in, body, err := h.parse(w, r)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_request")
		return native.Submit{}, nil, Task{}, false
	}
	var origin Task
	if strings.HasSuffix(r.URL.Path, "/remix") {
		origin, err = h.cfg.Repository.GetOwned(r.Context(), r.PathValue("id"), p.UserID)
		if err != nil || origin.Status != "completed" || origin.UpstreamID == "" {
			fail(w, http.StatusNotFound, "task_not_found")
			return native.Submit{}, nil, Task{}, false
		}
		in.OriginID = origin.UpstreamID
		in.Action = "remix"
		if in.Model == "" {
			in.Model = origin.Model
		}
	}
	if in.Model == "" {
		fail(w, http.StatusBadRequest, "model_required")
		return native.Submit{}, nil, Task{}, false
	}
	if !h.policy(w, r, p, in.Model) {
		return native.Submit{}, nil, Task{}, false
	}
	return in, body, origin, true
}

// selectTarget plans the request and picks the first candidate target whose
// provider matches the route family, the remix origin (if any), and has a
// registered adapter.
func (h *Handler) selectTarget(w http.ResponseWriter, r *http.Request, in native.Submit, body []byte, p gateway.Principal, origin Task) (*gateway.Request, gateway.Target, native.Adapter, bool) {
	req := &gateway.Request{ID: taskID(), Received: h.cfg.Now(), Model: in.Model, Body: body, Principal: p}
	req.PricingHeaders = pricingHeaders(r)
	targets, err := h.cfg.Planner.Plan(r.Context(), req)
	if err != nil {
		fail(w, http.StatusNotFound, "model_not_found")
		return nil, gateway.Target{}, nil, false
	}
	var target gateway.Target
	var adapter native.Adapter
	for _, candidate := range targets {
		if !routeFamily(r.URL.Path, candidate.Provider) {
			continue
		}
		if origin.ID != "" && (candidate.ChannelID != origin.ChannelID || candidate.CredentialID != origin.CredentialID) {
			continue
		}
		if a := h.cfg.Providers[candidate.Provider]; a != nil {
			target, adapter = candidate, a
			break
		}
	}
	if adapter == nil {
		fail(w, http.StatusNotFound, "task_provider_unavailable")
		return nil, gateway.Target{}, nil, false
	}
	req.Targets = []gateway.Target{target}
	return req, target, adapter, true
}

// acquireLease acquires a rate limit lease for the target when limits are
// configured. The returned release func, if non-nil, must be deferred by the
// caller so release timing matches the original inline defer.
func (h *Handler) acquireLease(w http.ResponseWriter, r *http.Request, req *gateway.Request, target gateway.Target) (func(), bool) {
	if h.cfg.Limits == nil {
		return nil, true
	}
	if err := h.cfg.Limits.Acquire(r.Context(), req, target); err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, gateway.ErrRateLimited) || errors.Is(err, gateway.ErrTargetBusy) {
			status = http.StatusTooManyRequests
		}
		fail(w, status, "task_limit_reached")
		return nil, false
	}
	release := func() {
		ctx, cancel := h.detached()
		defer cancel()
		if err := h.cfg.Limits.Release(ctx, req, target); err != nil {
			h.cfg.Logger.Error("task lease release failed", "task_id", req.ID, "error", err)
		}
	}
	return release, true
}

// reserveCredits reserves estimated credits for the request.
func (h *Handler) reserveCredits(w http.ResponseWriter, r *http.Request, req *gateway.Request) (Reservation, bool) {
	reservation, err := h.cfg.Settler.Reserve(r.Context(), req)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, gateway.ErrInsufficientCredits) {
			status = http.StatusPaymentRequired
		}
		fail(w, status, "task_reservation_failed")
		return Reservation{}, false
	}
	return reservation, true
}

// createTask builds the Task record and persists it. On persistence failure
// it compensates by finalizing the reservation as failed, mirroring the
// original inline rollback.
func (h *Handler) createTask(w http.ResponseWriter, req *gateway.Request, target gateway.Target, in native.Submit, body []byte, p gateway.Principal, reservation Reservation) (Task, bool) {
	t := Task{ID: req.ID, UserID: p.UserID, KeyID: p.KeyID, Group: p.Group, TargetGroup: target.Group, Model: req.Model, Body: body, PricingHeaders: req.PricingHeaders,
		Provider: target.Provider, ChannelID: target.ChannelID, CredentialID: target.CredentialID,
		UpstreamModel: target.UpstreamModel, Action: in.Action, Status: "submitting", CostState: "reserved",
		Reservation: reservation, CreatedAt: req.Received, UpdatedAt: req.Received, LeaseID: taskID()}
	if t.UpstreamModel == "" {
		t.UpstreamModel = t.Model
	}
	ctx, cancel := h.detached()
	err := h.cfg.Repository.Create(ctx, t)
	cancel()
	if err != nil {
		ctx, cancel = h.detached()
		_, refundErr := h.cfg.Settler.Finalize(ctx, req, reservation, native.Result{Status: "failed"})
		cancel()
		if refundErr != nil {
			h.cfg.Logger.Error("task create compensation failed", "task_id", t.ID, "error", refundErr)
		}
		fail(w, http.StatusServiceUnavailable, "task_storage_unavailable")
		return Task{}, false
	}
	return t, true
}

// runSubmit calls the provider adapter and updates t's status/result fields
// in place, exactly as the inline code previously did.
func (h *Handler) runSubmit(req *gateway.Request, target gateway.Target, adapter native.Adapter, in native.Submit, t *Task) (native.Result, error) {
	ctx, cancel := context.WithTimeout(native.WithRequest(context.Background(), req, target, h.cfg.Clients), h.cfg.SubmitTimeout)
	result, submitErr := adapter.Submit(ctx, target, in)
	cancel()
	result = sanitizeResult(result, target.Secret)
	var rejected *native.Rejected
	var invalid *native.InvalidRequest
	if submitErr != nil || (result.ID == "" && result.Status != "failed") {
		if errors.As(submitErr, &rejected) || errors.As(submitErr, &invalid) {
			t.Status = "failed"
			t.Error = "upstream rejected task"
			result = native.Result{Status: "failed", Error: t.Error}
		} else {
			t.Status = "submission_unknown"
			t.Error = "upstream acceptance could not be confirmed"
		}
	} else {
		t.apply(result)
	}
	return result, submitErr
}

// finishSubmit settles a failed submission, persists the final task state,
// reports the attempt to the planner, and writes the HTTP response.
func (h *Handler) finishSubmit(w http.ResponseWriter, r *http.Request, t *Task, result native.Result, submitErr error, target gateway.Target) {
	if t.Status == "failed" {
		ctx, cancel := h.detached()
		err := h.settle(ctx, t, result)
		cancel()
		if err != nil {
			h.cfg.Logger.Error("task refund pending", "task_id", t.ID, "error", err)
		}
	}
	t.UpdatedAt = h.cfg.Now()
	ctx, cancel := h.detached()
	err := h.cfg.Repository.Save(ctx, *t)
	cancel()
	h.cfg.Planner.Report(target, gateway.AttemptResult{OK: submitErr == nil, Status: providerStatus(submitErr)})
	if err != nil {
		h.cfg.Logger.Error("task acceptance persistence failed", "task_id", t.ID, "upstream_id", t.UpstreamID, "error", err)
		fail(w, http.StatusServiceUnavailable, "task_acceptance_not_persisted")
		return
	}
	w.Header().Set("X-Task-Id", t.ID)
	if t.Status == "submission_unknown" {
		fail(w, http.StatusBadGateway, "task_acceptance_unknown")
		return
	}
	if t.Status == "failed" {
		fail(w, http.StatusBadGateway, "task_rejected")
		return
	}
	h.respond(w, r, *t, true)
}

func routeFamily(path, provider string) bool {
	if strings.HasPrefix(path, "/suno/") {
		return provider == "suno"
	}
	if strings.HasPrefix(path, "/kling/") {
		return provider == "kling"
	}
	if strings.HasPrefix(path, "/jimeng/") {
		return provider == "jimeng"
	}
	return true
}

func providerStatus(err error) int {
	var rejected *native.Rejected
	if errors.As(err, &rejected) {
		return rejected.Status
	}
	if err == nil {
		return http.StatusOK
	}
	return http.StatusBadGateway
}
