// Package control composes the independently authenticated control-plane domains.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Principal struct {
	UserID int64
	Admin  bool
	Root   bool
}

type principalContextKey struct{}

type Config struct {
	PublicURL    string
	Authenticate func(*http.Request) (Principal, error)
	Ready        func(context.Context) error
}

type Server struct {
	mux *http.ServeMux
	cfg Config
	log *slog.Logger
}

func New(cfg Config, log *slog.Logger) (*Server, error) {
	if cfg.Authenticate == nil {
		return nil, errors.New("control: authentication is required")
	}
	if cfg.PublicURL != "" {
		u, err := url.Parse(cfg.PublicURL)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return nil, errors.New("control: public URL must be an absolute HTTP(S) URL")
		}
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Server{mux: http.NewServeMux(), cfg: cfg, log: log}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		Reply(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /readyz", s.ready)
	s.mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) {
		Reply(w, http.StatusOK, map[string]any{"version": "v3", "credits_per_unit": 1_000_000})
	})
	s.mux.HandleFunc("/api/", s.apiFallback)
	s.mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	return s, nil
}

func (s *Server) Mux() *http.ServeMux { return s.mux }

// RequireUser and RequireAdmin centralize session checks for domain registrars.
func (s *Server) RequireUser(next http.Handler) http.Handler {
	return s.authorize(next, false)
}

func (s *Server) RequireAdmin(next http.Handler) http.Handler {
	return s.authorize(next, true)
}

func (s *Server) RequireRoot(next http.Handler) http.Handler {
	return s.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := r.Context().Value(principalContextKey{}).(Principal)
		if !ok || !principal.Root {
			Fail(w, http.StatusForbidden, "forbidden", "需要根管理员权限")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (s *Server) authorize(next http.Handler, admin bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.cfg.Authenticate(r)
		if err != nil || p.UserID <= 0 {
			Fail(w, http.StatusUnauthorized, "authentication_required", "请先登录")
			return
		}
		if admin && !p.Admin {
			Fail(w, http.StatusForbidden, "forbidden", "需要管理员权限")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, p)))
	})
}

func (s *Server) Handler() http.Handler {
	return s.Guard(s.mux)
}

// Guard applies the same origin and response policy outside generated routing,
// including parameter-validation failures and static application responses.
func (s *Server) Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-New-Api-Version", "v3")
		if r.URL.Path == "/api" || len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			w.Header().Set("Cache-Control", "no-store")
		}
		// OIDC protocol POSTs use client credentials or bearer tokens, never
		// session cookies, and may originate from the configured community site.
		protocol := r.Method == http.MethodPost && (r.URL.Path == "/api/oidc/token" || r.URL.Path == "/api/oidc/userinfo")
		if !protocol && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && !s.sameOrigin(r) {
			Fail(w, http.StatusForbidden, "invalid_origin", "请求来源无效")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Preserve method failures when the application fallback is mounted at /.
func (s *Server) apiFallback(w http.ResponseWriter, r *http.Request) {
	var allowed []string
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		probe := r.Clone(r.Context())
		probe.Method = method
		_, pattern := s.mux.Handler(probe)
		if strings.Contains(pattern, " ") {
			allowed = append(allowed, method)
		}
	}
	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		Fail(w, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不支持")
		return
	}
	http.NotFound(w, r)
}

func (s *Server) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if s.cfg.PublicURL != "" {
		expected, _ := url.Parse(s.cfg.PublicURL)
		return u.Scheme == expected.Scheme && u.Host == expected.Host
	}
	return u.Host == r.Host && (u.Scheme == "https") == (r.TLS != nil)
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if s.cfg.Ready != nil {
		if err := s.cfg.Ready(ctx); err != nil {
			s.log.Warn("control readiness check failed", "err", err)
			Fail(w, http.StatusServiceUnavailable, "not_ready", "服务正在准备")
			return
		}
	}
	Reply(w, http.StatusOK, map[string]string{"status": "ready"})
}

func Reply(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    any    `json:"data,omitempty"`
	}{Success: true, Data: data})
}

func Fail(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message})
}
