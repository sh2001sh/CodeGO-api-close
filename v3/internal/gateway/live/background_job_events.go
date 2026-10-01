package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func (h *Handler) persistBackgroundEvent(ctx context.Context, job *BackgroundJob, req *gateway.Request, name string, data []byte) error {
	if !gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject() {
		return errors.New("live: invalid Responses background event")
	}
	root := gjson.ParseBytes(data)
	rawSnapshot, snapshot := backgroundEventSnapshot(root)
	if err := h.syncBackgroundUpstreamID(ctx, job, snapshot); err != nil {
		return err
	}
	if job.Native && job.UpstreamID == "" {
		return errors.New("live: native background acceptance lacks response ID")
	}
	if backgroundEventIsDuplicate(job, root, rawSnapshot, snapshot) {
		return nil
	}
	name, err := backgroundEventName(root, snapshot, name)
	if err != nil {
		return err
	}
	snapshot, err = h.mergeBackgroundNativeItem(job, root, snapshot)
	if err != nil {
		return err
	}
	applyBackgroundSnapshot(job, req, snapshot)
	applyBackgroundDelta(job, root)
	applyBackgroundTerminalStatus(job, name)
	payload := buildBackgroundEventPayload(job, root, name, data, rawSnapshot)
	if _, err := h.cfg.BackgroundJobs.Append(ctx, job.ID, job.LeaseID, BackgroundEvent{Type: name, Payload: payload}); err != nil {
		return err
	}
	if sequence := root.Get("sequence_number"); sequence.Exists() {
		job.LastUpstreamSequence = sequence.Int()
	}
	job.UpdatedAt = time.Now().UTC()
	return h.cfg.BackgroundJobs.Save(ctx, *job)
}

// backgroundEventSnapshot locates the response snapshot embedded in a background event payload,
// covering both the wrapped `{"response": {...}}` shape and a bare top-level response object.
func backgroundEventSnapshot(root gjson.Result) (rawSnapshot bool, snapshot gjson.Result) {
	rawSnapshot = root.Get("object").Str == "response" || (root.Get("id").Str != "" && root.Get("status").Str != "")
	snapshot = root.Get("response")
	if !snapshot.IsObject() && root.Get("object").Str == "response" {
		snapshot = root
	}
	if !snapshot.IsObject() && root.Get("id").Str != "" && root.Get("status").Str != "" {
		snapshot = root
	}
	return rawSnapshot, snapshot
}

// syncBackgroundUpstreamID validates the snapshot's response ID against the job's recorded
// upstream ID and, on first observation, durably records it before further processing.
func (h *Handler) syncBackgroundUpstreamID(ctx context.Context, job *BackgroundJob, snapshot gjson.Result) error {
	id := snapshot.Get("id").Str
	if id == "" {
		return nil
	}
	if !backgroundID(id) || (job.UpstreamID != "" && job.UpstreamID != id) {
		return errors.New("live: background upstream identity changed")
	}
	if job.UpstreamID == "" {
		job.UpstreamID, job.Status = id, "in_progress"
		if err := h.cfg.BackgroundJobs.Save(ctx, *job); err != nil {
			return err
		}
	}
	return nil
}

// backgroundEventIsDuplicate reports whether a native event is a replay of already-applied
// state: either an unordered snapshot resend, or a sequenced event at or behind the job's
// last-applied upstream sequence number.
func backgroundEventIsDuplicate(job *BackgroundJob, root gjson.Result, rawSnapshot bool, snapshot gjson.Result) bool {
	if job.Native && rawSnapshot && len(job.Snapshot) > 0 && !root.Get("sequence_number").Exists() {
		mapped, _ := sjson.SetBytes([]byte(snapshot.Raw), "id", job.ID)
		if bytes.Equal(mapped, backgroundSnapshot(*job)) {
			return true
		}
	}
	if job.Native && root.Get("sequence_number").Exists() && root.Get("sequence_number").Int() <= job.LastUpstreamSequence {
		return true
	}
	return false
}

// backgroundEventName derives and validates the canonical event type name, falling back to a
// name synthesized from the snapshot's status when the event carries none of its own.
func backgroundEventName(root, snapshot gjson.Result, name string) (string, error) {
	if value := root.Get("type").Str; value != "" {
		name = value
	}
	if name == "" {
		name = "response." + snapshot.Get("status").Str
	}
	if !strings.HasPrefix(name, "response.") && name != "error" {
		return "", errors.New("live: invalid background event type")
	}
	if strings.ContainsAny(name, "\r\n") {
		return "", errors.New("live: invalid background event name")
	}
	return name, nil
}

// mergeBackgroundNativeItem folds an output-item delta event into the job's running native
// snapshot, since native providers emit items incrementally rather than resending the whole
// response. Non-item events and non-native jobs pass the snapshot through unchanged.
func (h *Handler) mergeBackgroundNativeItem(job *BackgroundJob, root, snapshot gjson.Result) (gjson.Result, error) {
	item := root.Get("item")
	if !job.Native || !item.IsObject() {
		return snapshot, nil
	}
	partial := []byte(job.Snapshot)
	if !gjson.ValidBytes(partial) {
		partial, _ = json.Marshal(map[string]any{"id": job.UpstreamID, "object": "response", "status": "in_progress", "output": []any{}})
	}
	index := root.Get("output_index").Int()
	if index < 0 || index > 100000 {
		return snapshot, errors.New("live: invalid background output index")
	}
	partial, _ = sjson.SetRawBytes(partial, "output."+strconv.FormatInt(index, 10), []byte(item.Raw))
	if int64(len(partial)) > h.cfg.MaxBodyBytes {
		return snapshot, errors.New("live: background snapshot exceeds size limit")
	}
	return gjson.ParseBytes(partial), nil
}

// applyBackgroundSnapshot folds a response snapshot into job state: persisted snapshot bytes,
// status, usage, delivery and generated-byte accounting.
func applyBackgroundSnapshot(job *BackgroundJob, req *gateway.Request, snapshot gjson.Result) {
	if !snapshot.IsObject() {
		return
	}
	job.Snapshot = []byte(snapshot.Raw)
	status := snapshot.Get("status").Str
	if status == "queued" || status == "in_progress" || backgroundTerminal(status) {
		job.Status = status
	}
	// Reuse canonical usage/tool decoding while bypassing its queued/empty
	// semantic gate. Even failure events expose their reported usage.
	if usage := backgroundReadSnapshotUsage(req, snapshot); usage != nil {
		backgroundApplyUsage(job, *usage)
	}
	if backgroundOutputSemantic(snapshot.Get("output")) {
		job.Delivered = true
	}
	if backgroundTerminal(status) && job.GeneratedBytes == 0 {
		job.GeneratedBytes = backgroundOutputBytes(snapshot.Get("output"))
	}
}

// applyBackgroundDelta accounts for a streamed text delta's contribution to delivery state and,
// for native jobs, generated byte count.
func applyBackgroundDelta(job *BackgroundJob, root gjson.Result) {
	if delta := root.Get("delta"); delta.Type == gjson.String && delta.Str != "" {
		job.Delivered = true
		if job.Native {
			job.GeneratedBytes += int64(len(delta.Str))
		}
	}
}

// applyBackgroundTerminalStatus maps a terminal/failure event name onto the job's status field.
func applyBackgroundTerminalStatus(job *BackgroundJob, name string) {
	if name == "response.failed" || name == "error" {
		job.Status, job.Error = "failed", "background_upstream_error"
	}
	if name == "response.cancelled" {
		job.Status = "cancelled"
	}
	if name == "response.completed" {
		job.Status = "completed"
	}
	if name == "response.incomplete" {
		job.Status = "incomplete"
	}
}

// buildBackgroundEventPayload rewrites upstream-assigned response identifiers in the raw event
// payload to the job's externally stable ID, and for raw snapshot events replaces the body with
// the job's merged canonical snapshot.
func buildBackgroundEventPayload(job *BackgroundJob, root gjson.Result, name string, data []byte, rawSnapshot bool) []byte {
	payload := bytes.Clone(data)
	if root.Get("response").IsObject() {
		payload, _ = sjson.SetBytes(payload, "response.id", job.ID)
	}
	if root.Get("id").Str == job.UpstreamID && job.UpstreamID != "" {
		payload, _ = sjson.SetBytes(payload, "id", job.ID)
	}
	if root.Get("response_id").Exists() {
		payload, _ = sjson.SetBytes(payload, "response_id", job.ID)
	}
	if rawSnapshot {
		payload, _ = json.Marshal(map[string]any{"type": name, "response": json.RawMessage(backgroundSnapshot(*job))})
	}
	return payload
}

func backgroundReadSnapshotUsage(req *gateway.Request, snapshot gjson.Result) *gateway.Usage {
	decoded := (responses.Provider{}).Decode(req, &http.Response{Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(snapshot.Raw))})
	event, _ := decoded.Next()
	_ = decoded.Close()
	return event.Usage
}

func backgroundOutputBytes(output gjson.Result) int64 {
	var result int64
	for _, item := range output.Array() {
		result += int64(len(item.Get("arguments").Str))
		for _, field := range []string{"content", "summary"} {
			for _, part := range item.Get(field).Array() {
				result += int64(len(part.Get("text").Str) + len(part.Get("refusal").Str))
			}
		}
	}
	return result
}

func backgroundApplyUsage(job *BackgroundJob, usage gateway.Usage) {
	previous := job.Usage
	if usage.Estimated && job.UsageReported {
		tools := usage.ToolCalls
		usage = previous
		usage.ToolCalls = make(map[string]int64)
		for name, count := range tools {
			usage.ToolCalls[name] = count
		}
	}
	if len(previous.ToolCalls) > 0 {
		if usage.ToolCalls == nil {
			usage.ToolCalls = make(map[string]int64)
		}
		for name, count := range previous.ToolCalls {
			if usage.ToolCalls[name] < count {
				usage.ToolCalls[name] = count
			}
		}
	}
	job.Usage = usage
	job.UsageReported = job.UsageReported || !usage.Estimated
}

func backgroundOutputSemantic(output gjson.Result) bool {
	for _, item := range output.Array() {
		if typ := item.Get("type").Str; typ != "" && typ != "message" && typ != "reasoning" {
			return true
		}
		for _, field := range []string{"content", "summary"} {
			for _, part := range item.Get(field).Array() {
				if part.Get("text").Str != "" || part.Get("refusal").Str != "" || part.Get("audio").Exists() {
					return true
				}
			}
		}
	}
	return false
}
