package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

type Config struct {
	Authorizer      gateway.Authorizer
	Planner         gateway.Planner
	Settler         Settler
	Repository      TaskRepository
	ResolveTarget   TargetResolver
	Providers       map[string]native.Adapter
	Clients         gateway.ClientProvider
	Limits          gateway.LeaseController
	TrustedProxies  []netip.Prefix
	ValidateRequest func(gateway.Principal, string, *http.Request) error
	// ContentAuthorizer optionally permits a control-plane user session for
	// video content. All submit/fetch routes continue to require an API key.
	ContentAuthorizer func(*http.Request) (gateway.Principal, error)
	MaxBodyBytes      int64
	SubmitTimeout     time.Duration
	ReconcileTimeout  time.Duration
	Logger            *slog.Logger
	Now               func() time.Time
}

type Handler struct{ cfg Config }

func New(c Config) (*Handler, error) {
	if c.Authorizer == nil || c.Planner == nil {
		return nil, errors.New("workflow HTTP handler requires authorization and routing")
	}
	return NewReconciler(c)
}

// NewReconciler creates the worker polling entry without unnecessary HTTP
// authorization/routing caches. Use New when registering public HTTP routes.
func NewReconciler(c Config) (*Handler, error) {
	if c.Settler == nil || c.Repository == nil || c.ResolveTarget == nil {
		return nil, errors.New("workflow requires durable billing, repository and target resolver")
	}
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = 32 << 20
	}
	if c.SubmitTimeout <= 0 {
		c.SubmitTimeout = 90 * time.Second
	}
	if c.ReconcileTimeout <= 0 {
		c.ReconcileTimeout = 90 * time.Second
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Providers == nil {
		c.Providers = DefaultProviders(nil)
	}
	return &Handler{cfg: c}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /suno/submit/{action}", h.submit)
	mux.HandleFunc("POST /suno/fetch", h.batch)
	mux.HandleFunc("GET /suno/fetch/{id}", h.fetch)
	mux.HandleFunc("POST /v1/video/generations", h.submit)
	mux.HandleFunc("GET /v1/video/generations/{id}", h.fetch)
	mux.HandleFunc("POST /v1/videos", h.submit)
	mux.HandleFunc("POST /v1/videos/{id}/remix", h.submit)
	mux.HandleFunc("GET /v1/videos/{id}", h.fetch)
	mux.HandleFunc("GET /v1/videos/{id}/content", h.content)
	for _, action := range []string{"text2video", "image2video"} {
		mux.HandleFunc("POST /kling/v1/videos/"+action, func(w http.ResponseWriter, r *http.Request) { r.SetPathValue("action", action); h.submit(w, r) })
		mux.HandleFunc("GET /kling/v1/videos/"+action+"/{id}", h.fetch)
	}
	mux.HandleFunc("POST /jimeng/", h.jimeng)
}

func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) (gateway.Principal, bool) {
	if h.cfg.Authorizer == nil {
		fail(w, http.StatusServiceUnavailable, "auth_unavailable")
		return gateway.Principal{}, false
	}
	key := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if key == "" {
		key = r.Header.Get("X-Api-Key")
	}
	p, err := h.cfg.Authorizer.Authorize(r.Context(), key)
	if errors.Is(err, gateway.ErrAuthUnavailable) {
		fail(w, http.StatusServiceUnavailable, "auth_unavailable")
		return p, false
	}
	if key == "" || err != nil || p.UserID <= 0 || p.KeyID <= 0 {
		fail(w, http.StatusUnauthorized, "invalid_api_key")
		return p, false
	}
	return p, h.policy(w, r, p, "")
}

func fail(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"type": "task_error", "code": code, "message": strings.ReplaceAll(code, "_", " ")}})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Default().Error("task response write failed", "error", err)
	}
}

func taskID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("cryptographic task IDs unavailable")
	}
	return "task_" + hex.EncodeToString(b[:])
}

func (h *Handler) detached() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), h.cfg.ReconcileTimeout)
}
