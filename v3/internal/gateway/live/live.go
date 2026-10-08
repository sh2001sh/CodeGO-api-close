// Package live implements authenticated persistent file and live API transports.
package live

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
	"sync"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"golang.org/x/net/websocket"
)

type Resolver func(context.Context, int64, int64) (gateway.Target, error)

// Locator contains only identifiers. Secrets are resolved from current snapshots.
type Locator struct {
	ID           string    `json:"id"`
	UserID       int64     `json:"user_id"`
	KeyID        int64     `json:"key_id"`
	ChannelID    int64     `json:"channel_id"`
	CredentialID int64     `json:"credential_id"`
	Model        string    `json:"model"`
	UpstreamID   string    `json:"upstream_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

var ErrNotFound = errors.New("live: resource not found")

type Repository interface {
	Put(context.Context, Locator, time.Duration) error
	Get(context.Context, string, int64, int64) (Locator, error)
	Delete(context.Context, string, int64, int64) error
}

type Config struct {
	Auth              gateway.Authorizer
	Planner           gateway.Planner
	Settler           gateway.Settler
	Requests          gateway.RequestRecorder
	Limits            gateway.LeaseController
	Providers         map[string]gateway.Provider
	Resolve           Resolver
	Repository        Repository
	BackgroundJobs    BackgroundJobRepository
	BackgroundBilling BackgroundBilling
	ResolvePrincipal  PrincipalResolver
	ValidateRequest   func(gateway.Principal, string, *http.Request) error
	TargetPolicy      gateway.TargetPolicy
	RequestGuard      gateway.RequestGuard
	TrustedProxies    []netip.Prefix
	AuthFailures      gateway.AuthFailureController
	Files             FileStore
	DeliveryKey       []byte
	Client            *http.Client
	Clients           gateway.ClientProvider
	Logger            *slog.Logger
	SessionTimeout    time.Duration
	FinalizeTimeout   time.Duration
	LocatorTTL        time.Duration
	MaxBodyBytes      int64
}

type Handler struct {
	cfg                 Config
	backgroundSlotsOnce sync.Once
	backgroundSlots     chan struct{}
}

func New(cfg Config) (*Handler, error) {
	if cfg.Auth == nil || cfg.Planner == nil || cfg.Settler == nil || cfg.Repository == nil || cfg.Resolve == nil {
		return nil, errors.New("live: authorizer, planner, settler, repository and resolver are required")
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{}
	}
	client := *cfg.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	cfg.Client = &client
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.SessionTimeout <= 0 {
		cfg.SessionTimeout = time.Hour
	}
	if cfg.FinalizeTimeout <= 0 {
		cfg.FinalizeTimeout = 5 * time.Second
	}
	if cfg.LocatorTTL <= 0 {
		cfg.LocatorTTL = 7 * 24 * time.Hour
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 64 << 20
	}
	if len(cfg.DeliveryKey) > 0 && len(cfg.DeliveryKey) < 32 {
		return nil, errors.New("live: delivery signing key must have at least 32 bytes")
	}
	return &Handler{cfg: cfg}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	h.registerFiles(mux)
	h.registerBackground(mux)
	for _, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
		mux.HandleFunc("GET "+path, h.serveResponsesWebSocket)
	}
	mux.HandleFunc("GET /v1/realtime", h.serveRealtime)
}

func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) (gateway.Principal, bool) {
	address := h.clientIP(r)
	if h.cfg.AuthFailures != nil && h.cfg.AuthFailures.Blocked(address) {
		writeError(w, 429, "authentication_limited", "authentication is temporarily limited")
		return gateway.Principal{}, false
	}
	key := websocketAPIKey(r)
	if key == "" {
		if h.cfg.AuthFailures != nil {
			h.cfg.AuthFailures.Failed(address)
		}
		writeError(w, 401, "missing_api_key", "missing API key")
		return gateway.Principal{}, false
	}
	p, err := h.cfg.Auth.Authorize(r.Context(), key)
	if err != nil || p.UserID <= 0 || p.KeyID <= 0 {
		if h.cfg.AuthFailures != nil && !errors.Is(err, gateway.ErrAuthUnavailable) {
			h.cfg.AuthFailures.Failed(address)
		}
		status := 401
		if errors.Is(err, gateway.ErrAuthUnavailable) {
			status = 503
		}
		writeError(w, status, "authentication_failed", "API key cannot be authorized")
		return gateway.Principal{}, false
	}
	if err := h.validatePolicy(p, "", r); err != nil {
		writeError(w, 403, "request_not_permitted", err.Error())
		return gateway.Principal{}, false
	}
	return p, true
}

func websocketAPIKey(r *http.Request) string {
	key := ""
	if value := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(value), "bearer ") {
		key = strings.TrimSpace(value[7:])
	}
	// Realtime browser clients convey credentials in a protocol token.
	if key == "" {
		for _, value := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
			value = strings.TrimSpace(value)
			if strings.HasPrefix(value, "openai-insecure-api-key.") {
				key = strings.TrimPrefix(value, "openai-insecure-api-key.")
				break
			}
		}
	}
	return key
}

// Socket credentials are rechecked before admitting new work. A principal
// captured at handshake may have been revoked or had its policy changed.
func (h *Handler) authorizeSocket(ctx context.Context, conn *websocket.Conn, r *http.Request, model string) (gateway.Principal, bool) {
	p, err := h.cfg.Auth.Authorize(ctx, websocketAPIKey(r))
	if err != nil || p.UserID <= 0 || p.KeyID <= 0 {
		status := 401
		if errors.Is(err, gateway.ErrAuthUnavailable) {
			status = 503
		}
		_ = socketError(conn, status, "authentication_failed", "API key cannot be authorized")
		return gateway.Principal{}, false
	}
	if err := h.validatePolicy(p, model, r); err != nil {
		_ = socketError(conn, 403, "request_not_permitted", err.Error())
		return gateway.Principal{}, false
	}
	return p, true
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"type": "invalid_request_error", "code": code, "message": message}})
}

func requestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func (h *Handler) finalize(req *gateway.Request, out gateway.Outcome) {
	_ = h.finalizeResult(req, out)
}

func (h *Handler) finalizeResult(req *gateway.Request, out gateway.Outcome) error {
	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.FinalizeTimeout)
	defer cancel()
	err := h.cfg.Settler.Finalize(ctx, req, out)
	if err != nil {
		h.cfg.Logger.Error("live finalization failed", "request_id", req.ID, "err", err)
	}
	gateway.RecordRequest(h.cfg.Requests, req, out, err == nil)
	return err
}

func (h *Handler) reserve(w http.ResponseWriter, r *http.Request, req *gateway.Request) bool {
	if err := h.cfg.Settler.Reserve(r.Context(), req); err != nil {
		status := 503
		if errors.Is(err, gateway.ErrInsufficientCredits) {
			status = 402
		}
		writeError(w, status, "billing_unavailable", "unable to reserve credits")
		return false
	}
	return true
}

func (h *Handler) remember(ctx context.Context, req *gateway.Request, target gateway.Target, id string) error {
	return h.cfg.Repository.Put(ctx, Locator{ID: id, UserID: req.Principal.UserID, KeyID: req.Principal.KeyID, ChannelID: target.ChannelID, CredentialID: target.CredentialID, Model: req.Model, CreatedAt: time.Now().UTC()}, h.cfg.LocatorTTL)
}

// RememberResponse lets the main relay persist a background/continuation locator
// when its adapter observes an upstream response ID.
func (h *Handler) RememberResponse(ctx context.Context, req *gateway.Request, target gateway.Target, id string) error {
	return h.remember(ctx, req, target, id)
}
