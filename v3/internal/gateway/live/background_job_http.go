package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func (h *Handler) serveBackgroundJob(w http.ResponseWriter, r *http.Request, principal gateway.Principal, id string) {
	if h.cfg.BackgroundJobs == nil {
		writeError(w, 503, "background_unavailable", "background lookup is unavailable")
		return
	}
	job, err := h.cfg.BackgroundJobs.GetOwned(r.Context(), id, principal.UserID, principal.KeyID)
	if err != nil || job.ID != id || job.UserID != principal.UserID || job.KeyID != principal.KeyID {
		status := 503
		if errors.Is(err, ErrNotFound) || err == nil {
			status = 404
		}
		writeError(w, status, "response_not_found", "background response is unavailable")
		return
	}
	if err := h.validatePolicy(principal, "", r); err != nil {
		writeError(w, 403, "request_not_permitted", err.Error())
		return
	}
	if r.Method == http.MethodPost {
		job, err = h.cfg.BackgroundJobs.Cancel(r.Context(), id, principal.UserID, principal.KeyID)
		if err != nil {
			writeError(w, 503, "background_store_unavailable", "cancellation cannot be persisted")
			return
		}
		writeBackgroundSnapshot(w, job)
		return
	}
	query, err := backgroundQuery(r.URL.RawQuery, true)
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	if query.Get("stream") != "true" {
		writeBackgroundSnapshot(w, job)
		return
	}
	if !job.Stream {
		writeError(w, 400, "background_stream_not_enabled", "background response was not created with stream=true")
		return
	}
	cursor := int64(-1)
	if value := query.Get("starting_after"); value != "" {
		cursor, _ = strconv.ParseInt(value, 10, 64)
	}
	h.streamBackgroundJob(w, r, job, cursor)
}

func backgroundSnapshot(job BackgroundJob) []byte {
	data := []byte(job.Snapshot)
	if !gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject() {
		data, _ = json.Marshal(map[string]any{"id": job.ID, "object": "response", "model": job.Model, "created_at": job.CreatedAt.Unix(), "output": []any{}, "background": true})
	}
	status := job.Status
	if status == "submitting" || status == "unknown_acceptance" {
		status = "in_progress"
	}
	data, _ = sjson.SetBytes(data, "id", job.ID)
	data, _ = sjson.SetBytes(data, "status", status)
	if job.Error != "" {
		data, _ = sjson.SetBytes(data, "error", map[string]string{"type": "upstream_error", "code": job.Error, "message": "background execution requires attention"})
	}
	return data
}

func writeBackgroundSnapshot(w http.ResponseWriter, job BackgroundJob) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(backgroundSnapshot(job))
}

func (h *Handler) streamBackgroundJob(w http.ResponseWriter, r *http.Request, job BackgroundJob, cursor int64) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "response_stream_unavailable", "response cannot be streamed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.cfg.SessionTimeout)
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	flusher.Flush()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		events, err := h.cfg.BackgroundJobs.Events(ctx, job.ID, cursor, 100)
		if err != nil {
			_, _ = io.WriteString(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"code\":\"background_store_unavailable\"}}\n\n")
			flusher.Flush()
			return
		}
		for _, event := range events {
			payload, err := sjson.SetBytes(event.Payload, "sequence_number", event.Sequence)
			if err != nil {
				return
			}
			// Event types are parsed/persisted by this worker, not caller text.
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, payload); err != nil {
				return
			}
			cursor = event.Sequence
			flusher.Flush()
		}
		current, err := h.cfg.BackgroundJobs.GetOwned(ctx, job.ID, job.UserID, job.KeyID)
		if err != nil {
			return
		}
		terminalSeen := false
		for _, event := range events {
			if event.Type == "response."+current.Status {
				terminalSeen = true
			}
		}
		if backgroundTerminal(current.Status) && len(events) < 100 && (terminalSeen || current.Billed) {
			return
		}
		if current.Status == "unknown_acceptance" {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
