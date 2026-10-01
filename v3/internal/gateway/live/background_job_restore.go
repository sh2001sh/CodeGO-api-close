package live

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Append and Save are separate repository operations. Replay already durable
// events after a crash between them before choosing whether to poll or settle.
func (h *Handler) restoreBackgroundProgress(ctx context.Context, job *BackgroundJob, req *gateway.Request) error {
	cursor := int64(-1)
	seen := make(map[int64]bool)
	generated := int64(0)
	for {
		events, err := h.cfg.BackgroundJobs.Events(ctx, job.ID, cursor, 200)
		if err != nil {
			return err
		}
		for _, event := range events {
			cursor = event.Sequence
			if err := h.replayBackgroundEvent(job, req, event, seen, &generated); err != nil {
				return err
			}
		}
		if len(events) < 200 {
			break
		}
	}
	job.GeneratedBytes = max(job.GeneratedBytes, generated)
	return nil
}

// replayBackgroundEvent re-applies one durably persisted background event to job state during
// restore, mirroring the live effects persistBackgroundEvent had when the event was first
// recorded: sequence/delta/snapshot bookkeeping and terminal status transitions.
func (h *Handler) replayBackgroundEvent(job *BackgroundJob, req *gateway.Request, event BackgroundEvent, seen map[int64]bool, generated *int64) error {
	root := gjson.ParseBytes(event.Payload)
	if !gjson.ValidBytes(event.Payload) || !root.IsObject() {
		return errors.New("live: corrupt persisted background event")
	}
	if sequence := root.Get("sequence_number"); job.Native && sequence.Type == gjson.Number {
		n := sequence.Int()
		if seen[n] {
			return nil
		}
		seen[n] = true
		job.LastUpstreamSequence = max(job.LastUpstreamSequence, n)
	}
	if delta := root.Get("delta"); delta.Type == gjson.String && delta.Str != "" {
		*generated += int64(len(delta.Str))
		job.Delivered = true
	}
	if err := h.replayBackgroundEventSnapshot(job, req, root, generated); err != nil {
		return err
	}
	switch event.Type {
	case "response.completed":
		job.Status = "completed"
	case "response.incomplete":
		job.Status = "incomplete"
	case "response.cancelled":
		job.Status = "cancelled"
	case "response.failed":
		job.Status, job.Error = "failed", "background_upstream_error"
	case "error":
		if root.Get("error.code").Str == "background_acceptance_unknown" {
			job.Status, job.Error = "unknown_acceptance", "background_acceptance_unknown"
		} else {
			job.Status, job.Error = "failed", "background_upstream_error"
		}
	}
	return nil
}

// replayBackgroundEventSnapshot merges a replayed native output-item delta into job's running
// snapshot (same as the live path), then folds the resulting snapshot's status/usage/delivery
// into job state.
func (h *Handler) replayBackgroundEventSnapshot(job *BackgroundJob, req *gateway.Request, root gjson.Result, generated *int64) error {
	snapshot := root.Get("response")
	if item := root.Get("item"); item.IsObject() {
		partial := []byte(job.Snapshot)
		if !gjson.ValidBytes(partial) {
			partial, _ = json.Marshal(map[string]any{"id": job.ID, "object": "response", "status": "in_progress", "output": []any{}})
		}
		index := root.Get("output_index").Int()
		if index < 0 || index > 100000 {
			return errors.New("live: invalid persisted output index")
		}
		partial, _ = sjson.SetRawBytes(partial, "output."+strconv.FormatInt(index, 10), []byte(item.Raw))
		if int64(len(partial)) > h.cfg.MaxBodyBytes {
			return errors.New("live: background snapshot exceeds size limit")
		}
		snapshot = gjson.ParseBytes(partial)
	}
	if snapshot.IsObject() {
		job.Snapshot = []byte(snapshot.Raw)
		status := snapshot.Get("status").Str
		if status == "queued" || status == "in_progress" || backgroundTerminal(status) {
			job.Status = status
		}
		if usage := backgroundReadSnapshotUsage(req, snapshot); usage != nil {
			backgroundApplyUsage(job, *usage)
		}
		if backgroundOutputSemantic(snapshot.Get("output")) {
			job.Delivered = true
		}
		if backgroundTerminal(status) && *generated == 0 {
			*generated = backgroundOutputBytes(snapshot.Get("output"))
		}
	}
	return nil
}
