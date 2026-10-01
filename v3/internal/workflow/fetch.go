package workflow

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func (h *Handler) fetch(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r)
	if !ok {
		return
	}
	t, err := h.cfg.Repository.GetOwned(r.Context(), r.PathValue("id"), p.UserID)
	if errors.Is(err, ErrNotFound) {
		fail(w, http.StatusNotFound, "task_not_found")
		return
	}
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "task_storage_unavailable")
		return
	}
	if !h.policy(w, r, p, t.Model) {
		return
	}
	if t.CostState == "reserved" && t.Status != "submitting" && t.Status != "submission_unknown" {
		if err = h.reconcileOne(r.Context(), t.ID); err != nil && !errors.Is(err, ErrConflict) {
			fail(w, http.StatusBadGateway, "task_refresh_failed")
			return
		}
		t, err = h.cfg.Repository.GetOwned(r.Context(), t.ID, p.UserID)
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "task_storage_unavailable")
			return
		}
	}
	h.respond(w, r, t, false)
}

func (h *Handler) batch(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r)
	if !ok {
		return
	}
	var in struct {
		IDs []string `json:"ids"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&in); err != nil || len(in.IDs) > 100 {
		fail(w, http.StatusBadRequest, "invalid_task_ids")
		return
	}
	data := make([]any, 0, len(in.IDs))
	for _, id := range in.IDs {
		t, err := h.cfg.Repository.GetOwned(r.Context(), id, p.UserID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "task_storage_unavailable")
			return
		}
		if t.Provider == "suno" {
			if !h.policy(w, r, p, t.Model) {
				return
			}
			data = append(data, taskDTO(t))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": "success", "data": data, "message": ""})
}

func (h *Handler) jimeng(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("Action") {
	case "CVSync2AsyncSubmitTask":
		h.submit(w, r)
	case "CVSync2AsyncGetResult":
		var body struct {
			ID string `json:"task_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || body.ID == "" {
			fail(w, http.StatusBadRequest, "task_id_required")
			return
		}
		r.SetPathValue("id", body.ID)
		h.fetch(w, r)
	default:
		fail(w, http.StatusBadRequest, "invalid_jimeng_action")
	}
}

func (h *Handler) content(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorizeContent(w, r)
	if !ok {
		return
	}
	t, target, adapter, ok := h.resolveContentTarget(w, r, p)
	if !ok {
		return
	}
	ctx, cancel := h.detached()
	defer cancel()
	ctx = native.WithRequest(ctx, t.Request(), target, h.cfg.Clients)
	resp, err := adapter.Content(ctx, target, t.Native())
	if errors.Is(err, native.ErrContentUnsupported) && t.URL != "" {
		resp, err = h.fetchResult(ctx, t.URL, target)
	}
	if err != nil || resp == nil {
		fail(w, http.StatusBadGateway, "task_content_unavailable")
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			h.cfg.Logger.Error("task content close failed", "task_id", t.ID, "error", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		fail(w, http.StatusBadGateway, "task_content_unavailable")
		return
	}
	writeContentResponse(w, resp, t.ID, h.cfg.Logger)
}

// resolveContentTarget loads the owned task, enforces status/policy gates,
// and resolves the credential/adapter needed to fetch its content. It
// writes the failure response itself when ok is false.
func (h *Handler) resolveContentTarget(w http.ResponseWriter, r *http.Request, p gateway.Principal) (Task, gateway.Target, native.Adapter, bool) {
	t, err := h.cfg.Repository.GetOwned(r.Context(), r.PathValue("id"), p.UserID)
	if errors.Is(err, ErrNotFound) {
		fail(w, http.StatusNotFound, "task_not_found")
		return Task{}, gateway.Target{}, nil, false
	}
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "task_storage_unavailable")
		return Task{}, gateway.Target{}, nil, false
	}
	if t.Status != "completed" {
		fail(w, http.StatusConflict, "task_not_completed")
		return Task{}, gateway.Target{}, nil, false
	}
	if !h.policy(w, r, p, t.Model) {
		return Task{}, gateway.Target{}, nil, false
	}
	target, err := h.cfg.ResolveTarget(r.Context(), t.ChannelID, t.CredentialID)
	if err != nil || target.ChannelID != t.ChannelID || target.CredentialID != t.CredentialID || target.Provider != t.Provider {
		fail(w, http.StatusServiceUnavailable, "task_credential_unavailable")
		return Task{}, gateway.Target{}, nil, false
	}
	a := h.cfg.Providers[t.Provider]
	if a == nil {
		fail(w, http.StatusServiceUnavailable, "task_provider_unavailable")
		return Task{}, gateway.Target{}, nil, false
	}
	target.UpstreamModel = t.UpstreamModel
	target.Group = t.TargetGroup
	return t, target, a, true
}

// writeContentResponse copies the provider's content response headers and
// body to w.
func writeContentResponse(w http.ResponseWriter, resp *http.Response, taskID string, logger *slog.Logger) {
	for _, key := range []string{"Content-Type", "Content-Length", "Content-Disposition"} {
		if value := resp.Header.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, resp.Body); err != nil {
		logger.Error("task content write failed", "task_id", taskID, "error", err)
	}
}

func (h *Handler) authorizeContent(w http.ResponseWriter, r *http.Request) (p gateway.Principal, ok bool) {
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" || r.Header.Get("X-Api-Key") != "" || h.cfg.ContentAuthorizer == nil {
		return h.authorize(w, r)
	}
	p, err := h.cfg.ContentAuthorizer(r)
	if err != nil || p.UserID <= 0 {
		fail(w, http.StatusUnauthorized, "invalid_user_session")
		return p, false
	}
	return p, h.policy(w, r, p, "")
}
