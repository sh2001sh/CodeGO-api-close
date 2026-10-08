package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

// Reconcile is suitable for a River periodic worker. Leases serialize polling;
// billing idempotency covers a crash after settlement but before saving state.
func (h *Handler) Reconcile(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	tasks, err := h.cfg.Repository.Pending(ctx, limit)
	if err != nil {
		return 0, err
	}
	count := 0
	var failures []error
	for _, t := range tasks {
		if err := h.reconcileOne(ctx, t.ID); err != nil {
			if !errors.Is(err, ErrConflict) {
				failures = append(failures, fmt.Errorf("task %s: %w", t.ID, err))
			}
		} else {
			count++
		}
	}
	return count, errors.Join(failures...)
}

func (h *Handler) reconcileOne(parent context.Context, id string) error {
	ctx, cancel := context.WithTimeout(parent, h.cfg.ReconcileTimeout)
	defer cancel()
	t, err := h.cfg.Repository.Claim(ctx, id, taskID(), h.cfg.Now().Add(h.cfg.ReconcileTimeout+30*time.Second))
	if err != nil {
		return err
	}
	// Any recoverable poll failure releases the lease but preserves the hold.
	release := func(cause error) error {
		t.UpdatedAt = h.cfg.Now()
		saveCtx, cancel := h.detached()
		defer cancel()
		return errors.Join(cause, h.cfg.Repository.Save(saveCtx, t))
	}
	result := native.Result{ID: t.UpstreamID, Status: t.Status, Data: t.Data, URL: t.URL, Error: t.Error, Usage: t.Usage, Units: t.Units}
	if t.Status != "completed" && t.Status != "failed" {
		target, err := h.cfg.ResolveTarget(ctx, t.ChannelID, t.CredentialID)
		if err != nil {
			return release(errors.New("task credential unavailable"))
		}
		if target.ChannelID != t.ChannelID || target.CredentialID != t.CredentialID || target.Provider != t.Provider {
			return release(errors.New("task credential reference changed"))
		}
		target.UpstreamModel = t.UpstreamModel
		target.Group = t.TargetGroup
		adapter := h.cfg.Providers[t.Provider]
		if adapter == nil {
			return release(errors.New("task provider unavailable"))
		}
		result, err = adapter.Poll(native.WithRequest(ctx, t.Request(), target, h.cfg.Clients), target, t.Native())
		if err != nil {
			return release(err)
		}
		result = sanitizeResult(result, target.Secret)
		if result.ID != "" && result.ID != t.UpstreamID {
			return release(errors.New("provider returned another task"))
		}
		if result.Status != "queued" && result.Status != "in_progress" && result.Status != "completed" && result.Status != "failed" {
			return release(errors.New("invalid provider task state"))
		}
		t.apply(result)
	}
	if t.Status == "completed" || t.Status == "failed" {
		if err := h.settle(ctx, &t, result); err != nil {
			return release(err)
		}
	}
	t.UpdatedAt = h.cfg.Now()
	return h.cfg.Repository.Save(ctx, t)
}

func (h *Handler) settle(ctx context.Context, t *Task, result native.Result) error {
	if t.CostState != "reserved" {
		return nil
	}
	if result.Status != "completed" && result.Status != "failed" {
		return errors.New("cannot settle nonterminal task")
	}
	req := t.Request()
	out := OutcomeFor(result, &req.Targets[0])
	result.Usage = out.Usage
	if !out.Charge {
		result.Units = 0
	}
	actual, err := h.cfg.Settler.Finalize(ctx, req, t.Reservation, result)
	if err != nil {
		return err
	}
	if actual < 0 || (result.Status == "failed" && actual != 0) {
		return errors.New("invalid task billing settlement")
	}
	t.ActualCredits = actual
	if result.Status == "failed" {
		t.CostState = "refunded"
	} else {
		t.CostState = "settled"
	}
	atomic.StoreInt64(&req.SettledAmount, int64(actual))
	req.Attempts = []gateway.Attempt{{Target: req.Targets[0]}}
	gateway.RecordRequest(h.cfg.Requests, req, out, true)
	return nil
}

func (t *Task) apply(r native.Result) {
	if r.ID != "" {
		t.UpstreamID = r.ID
	}
	t.Status = r.Status
	if t.Status == "" {
		t.Status = "queued"
	}
	t.Data, t.URL, t.Error, t.Usage, t.Units = r.Data, r.URL, r.Error, r.Usage, r.Units
}
