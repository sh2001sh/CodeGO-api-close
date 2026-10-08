// Package adminops restores account favorites and protected administrative tools.
package adminops

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/settings"
)

type Crypto interface {
	catalog.Encrypter
	catalog.Decrypter
}
type Actor struct {
	UserID int64
	Role   string
}
type Authenticate func(*http.Request) (Actor, error)
type Config struct {
	HTTPClient              *http.Client
	DeploymentPublicURL     string
	DeploymentEnterpriseURL string
	DiskCacheDir            string
	LogDir                  string
	ActiveLog               func() string
	Now                     func() time.Time
}
type Server struct {
	pool     *pgxpool.Pool
	crypto   Crypto
	cfg      Config
	log      *slog.Logger
	settings *settings.Store
	requests atomic.Uint64
	started  time.Time
}

func New(pool *pgxpool.Pool, crypto Crypto, cfg Config, log *slog.Logger) *Server {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = defaultToolClient()
	}
	if log == nil {
		log = slog.Default()
	}
	return &Server{pool: pool, crypto: crypto, cfg: cfg, log: log, settings: settings.New(pool, crypto), started: cfg.Now()}
}

type actorHandler func(http.ResponseWriter, *http.Request, Actor)

func (s *Server) Register(mux *http.ServeMux, auth Authenticate) {
	s.registerFavorites(mux, auth)
	s.registerRatio(mux, auth)
	s.registerDeployments(mux, auth)
	s.registerPerformance(mux, auth)
}
func (s *Server) protected(auth Authenticate, role string, next actorHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if auth == nil {
			fail(w, 401, "unauthorized", "Authentication is required")
			return
		}
		actor, err := auth(r)
		if err != nil || actor.UserID <= 0 {
			fail(w, 401, "unauthorized", "Authentication is required")
			return
		}
		if role == "root" && actor.Role != "root" || role == "admin" && actor.Role != "root" && actor.Role != "admin" {
			fail(w, 403, "forbidden", "Insufficient permissions")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			origin := r.Header.Get("Origin")
			u, e := url.Parse(origin)
			if origin != "" && (e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host != r.Host) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "forbidden", "Cross-origin changes are forbidden")
				return
			}
		}
		s.requests.Add(1)
		next(w, r, actor)
	}
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || !errors.Is(d.Decode(new(any)), io.EOF) {
		fail(w, 400, "invalid_payload", "Expected one valid JSON object")
		return false
	}
	return true
}
func respond(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Success bool   `json:"success"`
		Data    any    `json:"data"`
		Message string `json:"message"`
	}{true, data, ""})
}
func fail(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}{false, code, message})
}
func (s *Server) dbError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Resource does not exist")
		return
	}
	s.log.Error("administrative storage failed", "err", err)
	fail(w, 503, "storage_unavailable", "Administrative storage is unavailable")
}
func pagination(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	p, n := 1, 20
	for key, dst := range map[string]*int{"page": &p, "page_size": &n} {
		if raw := r.URL.Query().Get(key); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < 1 || key == "page_size" && v > 100 || key == "page" && v > 1000000 {
				fail(w, 400, "invalid_page", "Invalid pagination")
				return 0, 0, false
			}
			*dst = v
		}
	}
	return p, n, true
}
