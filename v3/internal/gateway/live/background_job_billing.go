package live

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func (h *Handler) finalizeBackgroundJob(parent context.Context, job *BackgroundJob, req *gateway.Request, target *gateway.Target) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), h.cfg.FinalizeTimeout)
	defer cancel()
	if _, err := h.cfg.BackgroundJobs.Claim(ctx, job.ID, job.LeaseID, backgroundLeaseTTL); err != nil {
		return err
	}
	job.UpdatedAt = time.Now().UTC()
	job.Snapshot = backgroundSnapshot(*job)
	if err := h.cfg.BackgroundJobs.Save(ctx, *job); err != nil {
		return err
	}
	if err := h.ensureBackgroundTerminalEvent(ctx, *job); err != nil {
		return err
	}
	estimate := gateway.Usage{PromptTokens: (int64(len(job.Body)) + 3) / 4, CompletionTokens: (job.GeneratedBytes + 3) / 4, Estimated: true}
	observation := gateway.Observation{Delivered: job.Delivered, Estimate: estimate, ClientCanceled: job.Status == "cancelled", TimedOut: job.Error == "upstream_timeout"}
	if job.UsageReported || len(job.Usage.ToolCalls) > 0 {
		observation.Usage = &job.Usage
	}
	if job.Status == "failed" {
		observation.Err = backgroundFailure(job.Error)
	}
	out := gateway.Decide(observation)
	if target == nil {
		target = &gateway.Target{ChannelID: job.ChannelID, CredentialID: job.CredentialID}
	}
	out.Target = target
	if job.TargetGroup != "" {
		copyTarget := *target
		copyTarget.Group = job.TargetGroup
		out.Target = &copyTarget
	}
	req.PersistedAttempts = job.AttemptsCount
	err := h.cfg.BackgroundBilling.Finalize(ctx, req, job.Reservation, out)
	if err != nil {
		return err
	}
	gateway.RecordRequest(h.cfg.Requests, req, out, true)
	job.Billed = true
	return h.cfg.BackgroundJobs.Save(ctx, *job)
}

func (h *Handler) markBackgroundUnknown(ctx context.Context, job *BackgroundJob) error {
	job.Status, job.Error = "unknown_acceptance", "background_acceptance_unknown"
	if err := h.cfg.BackgroundJobs.Save(ctx, *job); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"type": "error", "response_id": job.ID, "error": map[string]string{"type": "upstream_error", "code": job.Error, "message": "upstream acceptance is unknown; the request will not be resubmitted"}})
	if _, err := h.cfg.BackgroundJobs.Append(ctx, job.ID, job.LeaseID, BackgroundEvent{Type: "error", Payload: payload}); err != nil {
		return err
	}
	return errBackgroundUnknown
}

func (h *Handler) ensureBackgroundTerminalEvent(ctx context.Context, job BackgroundJob) error {
	want := "response." + job.Status
	var last string
	cursor := int64(-1)
	for {
		events, err := h.cfg.BackgroundJobs.Events(ctx, job.ID, cursor, 200)
		if err != nil {
			return err
		}
		for _, event := range events {
			last, cursor = event.Type, event.Sequence
		}
		if len(events) < 200 {
			break
		}
	}
	if last == want {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{"type": want, "response": json.RawMessage(backgroundSnapshot(job))})
	_, err := h.cfg.BackgroundJobs.Append(ctx, job.ID, job.LeaseID, BackgroundEvent{Type: want, Payload: payload})
	return err
}
