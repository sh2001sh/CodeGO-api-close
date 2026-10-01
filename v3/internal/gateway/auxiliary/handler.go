package auxiliary

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

type Handler struct {
	cfg      Config
	adapters map[string]Adapter
}

func New(cfg Config) (*Handler, error) {
	if cfg.Authorizer == nil || cfg.Planner == nil || cfg.Settler == nil {
		return nil, errors.New("auxiliary: authorizer, planner and settler required")
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 32 << 20
	}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = 64 << 20
	}
	if cfg.RelayTimeout <= 0 {
		cfg.RelayTimeout = 30 * time.Minute
	}
	if cfg.DrainTimeout <= 0 {
		cfg.DrainTimeout = 30 * time.Second
	}
	if cfg.FinalizeTimeout <= 0 {
		cfg.FinalizeTimeout = 5 * time.Second
	}
	if cfg.Transports == nil {
		cfg.Transports = httpx.NewPool(httpx.TransportConfig{})
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	adapters := defaultAdapters()
	for id, adapter := range cfg.Adapters {
		adapters[id] = adapter
	}
	return &Handler{cfg: cfg, adapters: adapters}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	for _, op := range []Operation{Images, ImageEdits, Edits, Embeddings, Transcriptions, Translations, Speech, Rerank, Moderations, Completions, Compact, Search} {
		mux.Handle("POST /v1/"+string(op), h)
	}
	mux.Handle("POST /v1/engines/{model}/embeddings", h)
	for _, prefix := range []string{"", "/backend-api/codex"} {
		mux.Handle("POST "+prefix+"/responses/compact", h)
		mux.Handle("POST "+prefix+"/alpha/search", h)
	}
}

// Wrap dispatches Gemini auxiliary actions sharing the core chat route pattern.
func (h *Handler) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && geminiOperation(r.URL.Path) != "" {
			h.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var id [12]byte
	_, _ = rand.Read(id[:])
	req := &gateway.Request{ID: "req_" + hex.EncodeToString(id[:]), Received: time.Now()}
	w.Header().Set("X-Request-Id", req.ID)
	in, err := h.parse(w, r, req)
	if err != nil {
		writeError(w, err)
		return
	}
	if err = h.authorize(r, req); err != nil {
		writeError(w, err)
		return
	}
	if h.cfg.RequestGuard != nil {
		if err = h.cfg.RequestGuard(r.Context(), req); err != nil {
			var denied *gateway.UpstreamError
			if !errors.As(err, &denied) {
				err = failure(http.StatusServiceUnavailable, "request_guard_unavailable", "request admission is temporarily unavailable")
			}
			writeError(w, err)
			return
		}
	}
	targets, err := h.cfg.Planner.Plan(r.Context(), req)
	if err != nil || len(targets) == 0 {
		writeError(w, failure(503, "no_available_channel", "no channel available for this model"))
		return
	}
	req.Targets = targets
	if denied := h.targetPolicyFailure(req, targets[0]); denied != nil {
		writeError(w, denied)
		return
	}
	if in.Operation == Compact {
		req.Model = strings.TrimSuffix(req.Model, "-openai-compact") + "-openai-compact"
	}
	if err = h.cfg.Settler.Reserve(r.Context(), req); err != nil {
		status, code, message := 503, "billing_unavailable", "billing is temporarily unavailable"
		if errors.Is(err, gateway.ErrInsufficientCredits) {
			status, code, message = 402, "insufficient_credits", "not enough credits"
		}
		writeError(w, failure(status, code, message))
		return
	}
	out := h.execute(w, r, req, in)
	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.FinalizeTimeout)
	defer cancel()
	if err := h.cfg.Settler.Finalize(ctx, req, out); err != nil {
		h.cfg.Logger.Error("auxiliary finalize failed", "request_id", req.ID, "terminal", out.Terminal.String(), "err", err)
	}
}

func (h *Handler) authorize(r *http.Request, req *gateway.Request) error {
	key := ""
	if value := r.Header.Get("Authorization"); value != "" {
		if token, ok := strings.CutPrefix(value, "Bearer "); ok {
			key = strings.TrimSpace(token)
		}
	} else {
		for _, name := range []string{"X-Api-Key", "X-Goog-Api-Key"} {
			if key = strings.TrimSpace(r.Header.Get(name)); key != "" {
				break
			}
		}
		if key == "" {
			key = strings.TrimSpace(r.URL.Query().Get("key"))
		}
	}
	address := clientAddress(r)
	if h.cfg.AuthFailures != nil && h.cfg.AuthFailures.Blocked(address) {
		return failure(429, "authentication_rate_limited", "too many failed authentication attempts")
	}
	if key == "" {
		return failure(401, "missing_api_key", "missing API key")
	}
	principal, err := h.cfg.Authorizer.Authorize(r.Context(), key)
	if errors.Is(err, gateway.ErrAuthUnavailable) {
		return failure(503, "auth_unavailable", "authorization temporarily unavailable")
	}
	if err != nil {
		if h.cfg.AuthFailures != nil {
			h.cfg.AuthFailures.Failed(address)
		}
		return failure(401, "invalid_api_key", "invalid API key")
	}
	req.Principal = principal
	if err := gateway.ValidateRequestPolicy(principal, req.Model, r, h.cfg.TrustedProxies); err != nil {
		return err
	}
	if h.cfg.ValidateRequest != nil {
		return h.cfg.ValidateRequest(principal, req.Model, r)
	}
	return nil
}
