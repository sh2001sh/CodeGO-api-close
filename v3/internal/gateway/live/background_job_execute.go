package live

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func (h *Handler) executeBackgroundJob(parent context.Context, job BackgroundJob) error {
	req := h.buildBackgroundRequest(job)
	policyErr := h.resolveBackgroundPrincipalPolicy(parent, &job, req)
	if err := h.cfg.BackgroundBilling.Refresh(parent, req, job.Reservation); err != nil {
		return err
	}
	if err := h.restoreBackgroundProgress(parent, &job, req); err != nil {
		return err
	}
	if err, done := h.handleBackgroundRestoreOutcome(parent, &job, req); done {
		return err
	}
	target, err, done := h.resolveBackgroundTarget(parent, &job, req, policyErr)
	if done {
		return err
	}
	req.Targets = []gateway.Target{target}
	ctx, timeoutCancel := context.WithTimeout(parent, h.cfg.SessionTimeout)
	defer timeoutCancel()
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var requested atomic.Bool
	requested.Store(job.CancelRequested)
	stop := h.startBackgroundLeaseMonitor(parent, &job, req, cancel, &requested)
	if h.cfg.Limits != nil {
		if err := h.cfg.Limits.Acquire(ctx, req, target); err != nil {
			if monitorErr := stop(); monitorErr != nil {
				return monitorErr
			}
			// This is admission failure before submission: retry the durable queue.
			return err
		}
		defer func() {
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), h.cfg.FinalizeTimeout)
			defer releaseCancel()
			if err := h.cfg.Limits.Release(releaseCtx, req, target); err != nil {
				h.cfg.Logger.Error("background worker lease release failed", "request_id", job.ID, "err", err)
			}
		}()
	}
	var executionErr error
	if job.UpstreamID != "" && job.Native {
		executionErr = h.pollNativeBackground(ctx, &job, req, target, requested.Load())
	} else {
		executionErr = h.submitBackground(ctx, &job, req, target)
	}
	monitorErr := stop()
	if monitorErr != nil {
		return monitorErr
	}
	job.CancelRequested = job.CancelRequested || requested.Load()
	if errors.Is(executionErr, ErrBackgroundLeaseConflict) {
		return executionErr
	}
	return h.finalizeBackgroundExecutionResult(parent, ctx, &job, req, target, executionErr)
}

// buildBackgroundRequest constructs the gateway request mirrored from a durable background job.
func (h *Handler) buildBackgroundRequest(job BackgroundJob) *gateway.Request {
	req := &gateway.Request{ID: job.ID, Received: job.CreatedAt, Protocol: gateway.ProtocolResponses, Body: job.Body, Model: job.Model,
		Stream: true, Path: job.Path, PricingHeaders: job.PricingHeaders, Principal: gateway.Principal{UserID: job.UserID, KeyID: job.KeyID, Group: job.Group}}
	if req.Path == "" {
		req.Path = "/v1/responses"
	}
	return req
}

// resolveBackgroundPrincipalPolicy re-resolves the job's principal and validates policy against it,
// returning any failure without acting on it (the caller folds it in with route resolution).
func (h *Handler) resolveBackgroundPrincipalPolicy(parent context.Context, job *BackgroundJob, req *gateway.Request) error {
	principal, policyErr := h.cfg.ResolvePrincipal(parent, job.UserID, job.KeyID)
	if policyErr == nil && (principal.UserID != job.UserID || principal.KeyID != job.KeyID) {
		policyErr = errors.New("live: background principal identity mismatch")
	}
	if policyErr == nil {
		principal.Group = job.Group
		req.Principal = principal
		policyRequest := &http.Request{RemoteAddr: net.JoinHostPort(job.ClientIP, "0"), Header: make(http.Header), URL: &url.URL{Path: req.Path}}
		policyErr = h.validatePolicy(principal, job.Model, policyRequest)
	}
	return policyErr
}

// handleBackgroundRestoreOutcome checks the job status after progress restore for terminal,
// unknown-acceptance, interrupted-local or already-cancelled outcomes that finish execution
// without reaching upstream. done reports whether the caller must return err immediately.
func (h *Handler) handleBackgroundRestoreOutcome(parent context.Context, job *BackgroundJob, req *gateway.Request) (err error, done bool) {
	if backgroundTerminal(job.Status) {
		return h.finalizeBackgroundJob(parent, job, req, nil), true
	}
	if job.Status == "unknown_acceptance" {
		return errBackgroundUnknown, true
	}
	if (job.Status == "submitting" || job.Status == "in_progress") && job.UpstreamID == "" {
		return h.markBackgroundUnknown(parent, job), true
	}
	if !job.Native && job.Status == "in_progress" {
		// A local provider with store=false cannot replay a lost TCP generation.
		// Settle the durable partial usage instead of executing the request twice.
		job.Status, job.Error = "failed", "background_worker_interrupted"
		return h.finalizeBackgroundJob(parent, job, req, nil), true
	}
	if job.CancelRequested && job.Status == "queued" && job.UpstreamID == "" {
		job.Status = "cancelled"
		return h.finalizeBackgroundJob(parent, job, req, nil), true
	}
	return nil, false
}

// resolveBackgroundTarget resolves the job's route and folds in the already-computed policy
// failure. For accepted native jobs it keeps polling the (now unauthorized) target to drive it
// to cancellation instead of abandoning it; otherwise it fails and finalizes the job in place.
// done reports whether the caller must return err immediately.
func (h *Handler) resolveBackgroundTarget(parent context.Context, job *BackgroundJob, req *gateway.Request, policyErr error) (target gateway.Target, err error, done bool) {
	target, routeErr := h.cfg.Resolve(parent, job.ChannelID, job.CredentialID)
	if routeErr == nil && (target.ChannelID != job.ChannelID || target.CredentialID != job.CredentialID) {
		routeErr = errors.New("live: background route identity mismatch")
	}
	if routeErr == nil && policyErr == nil {
		if failure := h.targetPolicyFailure(req, target); failure != nil {
			policyErr = failure
		}
	}
	if routeErr != nil || policyErr != nil {
		if job.UpstreamID != "" && job.Native {
			if routeErr != nil {
				return target, routeErr, true
			}
			// A revoked policy must stop accepted native work before refunding.
			job.CancelRequested = true
			return target, nil, false
		}
		job.Status, job.Error = "failed", "background_route_not_permitted"
		return target, h.finalizeBackgroundJob(parent, job, req, nil), true
	}
	return target, nil, false
}

// startBackgroundLeaseMonitor periodically renews the job's durable lease and refreshes its
// billing reservation while execution is in flight, cancelling ctx (via cancel) if the lease is
// lost or the job's cancellation flag is observed. The returned stop func halts the monitor and
// surfaces any lease/billing error it hit.
func (h *Handler) startBackgroundLeaseMonitor(parent context.Context, job *BackgroundJob, req *gateway.Request, cancel context.CancelCauseFunc, requested *atomic.Bool) func() error {
	monitorCtx, stopMonitor := context.WithCancel(parent)
	monitorDone := make(chan struct{})
	monitorErrors := make(chan error, 1)
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		refreshAt := time.Now().Add(10 * time.Second)
		for {
			select {
			case <-monitorCtx.Done():
				return
			case <-ticker.C:
			}
			current, err := h.cfg.BackgroundJobs.Claim(monitorCtx, job.ID, job.LeaseID, backgroundLeaseTTL)
			if err == nil && time.Now().After(refreshAt) {
				err = h.cfg.BackgroundBilling.Refresh(monitorCtx, req, job.Reservation)
				refreshAt = time.Now().Add(10 * time.Second)
			}
			if err != nil {
				if monitorCtx.Err() == nil {
					monitorErrors <- err
					cancel(err)
				}
				return
			}
			if current.CancelRequested {
				requested.Store(true)
				cancel(errBackgroundCanceled)
				return
			}
		}
	}()
	return func() error {
		stopMonitor()
		<-monitorDone
		select {
		case err := <-monitorErrors:
			return err
		default:
			return nil
		}
	}
}

// finalizeBackgroundExecutionResult interprets the outcome of the upstream execution attempt:
// it finalizes terminal jobs, drives an accepted native job to cancellation if requested, or
// persists the resumable native job state for the next lease. ctx is the execution context
// (used only to detect its own deadline); parent is used for post-execution durable writes so
// they are not aborted by ctx's cancellation.
func (h *Handler) finalizeBackgroundExecutionResult(parent, ctx context.Context, job *BackgroundJob, req *gateway.Request, target gateway.Target, executionErr error) error {
	if backgroundTerminal(job.Status) {
		return h.finalizeBackgroundJob(parent, job, req, &target)
	}
	if job.Status == "unknown_acceptance" {
		return errBackgroundUnknown
	}
	if job.Status == "submitting" && job.UpstreamID == "" {
		return h.markBackgroundUnknown(context.WithoutCancel(parent), job)
	}
	if job.CancelRequested && job.Native && job.UpstreamID != "" {
		cancelCtx, cancelCancel := context.WithTimeout(context.WithoutCancel(parent), min(h.cfg.SessionTimeout, 30*time.Second))
		executionErr = h.pollNativeBackground(cancelCtx, job, req, target, true)
		cancelCancel()
		if backgroundTerminal(job.Status) {
			return h.finalizeBackgroundJob(parent, job, req, &target)
		}
	}
	if job.Native && job.UpstreamID != "" {
		// Accepted native jobs survive stream EOF, shutdown or polling failure.
		// The next lease resumes from the durable upstream ID/cursor.
		if err := h.cfg.BackgroundJobs.Save(context.WithoutCancel(parent), *job); err != nil {
			return err
		}
		// No worker remains after this return. Shorten its claim so the next
		// native poll need not wait the full running-stream lease duration.
		if executionErr == nil {
			if _, err := h.cfg.BackgroundJobs.Claim(context.WithoutCancel(parent), job.ID, job.LeaseID, time.Millisecond); err != nil {
				return err
			}
		}
		return executionErr
	}
	job.Status, job.Error = "failed", "background_upstream_error"
	if job.CancelRequested {
		job.Status, job.Error = "cancelled", ""
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		job.Error = "upstream_timeout"
	}
	return h.finalizeBackgroundJob(parent, job, req, &target)
}
