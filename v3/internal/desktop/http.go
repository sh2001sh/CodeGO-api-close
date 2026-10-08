package desktop

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/identity"
)

func (s *Service) Register(mux *http.ServeMux) { mux.Handle("/api/desktop/", s.Handler()) }

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/desktop/auth/session", s.startHTTP)
	mux.HandleFunc("GET /api/desktop/auth/session", s.viewHTTP)
	mux.HandleFunc("POST /api/desktop/auth/poll", s.pollHTTP)
	mux.HandleFunc("POST /api/desktop/auth/approve", s.decideHTTP)
	mux.HandleFunc("POST /api/desktop/auth/reject", s.decideHTTP)
	mux.HandleFunc("GET /api/desktop/devices", s.devicesHTTP)
	mux.HandleFunc("DELETE /api/desktop/devices/{id}", s.devicesHTTP)
	mux.HandleFunc("GET /api/desktop/authorized-devices", s.devicesHTTP)
	mux.HandleFunc("DELETE /api/desktop/authorized-devices/{id}", s.devicesHTTP)
	mux.HandleFunc("GET /api/desktop/account/summary", s.summaryHTTP)
	mux.HandleFunc("GET /api/desktop/usage/logs", s.logsHTTP)
	mux.HandleFunc("GET /api/desktop/usage/trends", s.trendsHTTP)
	mux.HandleFunc("GET /api/desktop/groups", s.groupsHTTP)
	mux.HandleFunc("GET /api/desktop/group-status", s.groupStatusHTTP)
	mux.HandleFunc("GET /api/desktop/pricing", s.pricingHTTP)
	mux.HandleFunc("GET /api/desktop/tokens", s.keysHTTP)
	mux.HandleFunc("POST /api/desktop/tokens", s.keysHTTP)
	mux.HandleFunc("PUT /api/desktop/tokens", s.keysHTTP)
	mux.HandleFunc("DELETE /api/desktop/tokens/{id}", s.keysHTTP)
	mux.HandleFunc("POST /api/desktop/tokens/{id}/key", s.keyHTTP)
	mux.HandleFunc("PUT /api/desktop/tokens/{id}/group", s.keyHTTP)
	mux.HandleFunc("POST /api/desktop/tokens/ensure", s.ensureHTTP)
	mux.HandleFunc("GET /api/desktop/tokens/{id}/config", s.configHTTP)
	mux.HandleFunc("GET /api/desktop/config/template", s.templateHTTP)
	mux.HandleFunc("GET /api/desktop/config/templates", s.templateHTTP)
	mux.HandleFunc("POST /api/desktop/import/deeplink", s.importHTTP)
	mux.HandleFunc("GET /api/desktop/import/config", s.consumeHTTP)
	mux.HandleFunc("GET /api/desktop/service/status", s.statusHTTP)
	mux.HandleFunc("POST /api/desktop/diagnostics/report", s.reportHTTP)
	mux.HandleFunc("POST /api/desktop/telemetry/events", s.reportHTTP)
	mux.HandleFunc("GET /api/desktop/release/latest", s.releaseHTTP)
	mux.HandleFunc("GET /api/desktop/release/latest.json", s.releaseHTTP)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != "GET" && r.Method != "HEAD" && !s.sameOrigin(r) {
			reply(w, nil, ErrDenied)
			return
		}
		if strings.Contains(r.URL.Path, "/auth/") && !s.limiter.allow(r, s.cfg.Now()) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"success":false,"message":"请求过于频繁"}`))
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Service) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if s.cfg.PublicURL == "" {
		return u.Host == r.Host
	}
	want, err := url.Parse(s.cfg.PublicURL)
	return err == nil && u.Scheme == want.Scheme && u.Host == want.Host
}
func decode(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return ErrInvalid
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}
func reply(w http.ResponseWriter, data any, err error) {
	status := 200
	message := ""
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalid), errors.Is(err, identity.ErrInvalidInput):
			status = 400
			message = "参数无效"
		case errors.Is(err, ErrUnauthenticated), errors.Is(err, identity.ErrCredentials):
			status = 401
			message = "授权已失效，请重新登录"
		case errors.Is(err, ErrDenied), errors.Is(err, identity.ErrForbidden):
			status = 403
			message = "授权无效或权限不足"
		case errors.Is(err, ErrMissing), errors.Is(err, identity.ErrNotFound):
			status = 404
			message = "记录不存在"
		default:
			status = 503
			message = "桌面服务暂时不可用"
			slog.Error("desktop request failed", "err", err)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": err == nil, "message": message, "data": data})
}
func positiveID(raw string) int64 {
	n, _ := strconv.ParseInt(raw, 10, 64)
	if n < 0 {
		return 0
	}
	return n
}
func (s *Service) device(w http.ResponseWriter, r *http.Request, scope string) (Device, bool) {
	d, err := s.Authenticate(r, scope)
	if err != nil {
		reply(w, nil, err)
		return Device{}, false
	}
	w.Header().Set("Auth-Version", "desktop-v1")
	return d, true
}
func (s *Service) browser(w http.ResponseWriter, r *http.Request) (identity.User, bool) {
	if s.id == nil {
		reply(w, nil, ErrDenied)
		return identity.User{}, false
	}
	u, err := s.id.AuthenticateRequest(r)
	if err != nil {
		reply(w, nil, err)
		return identity.User{}, false
	}
	return u, true
}
func (s *Service) startHTTP(w http.ResponseWriter, r *http.Request) {
	var in StartInput
	if err := decode(w, r, &in); err != nil {
		reply(w, nil, err)
		return
	}
	out, err := s.Start(r.Context(), in)
	reply(w, out, err)
}
func (s *Service) viewHTTP(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.browser(w, r); !ok {
		return
	}
	out, err := s.View(r.Context(), r.URL.Query().Get("session_id"), r.URL.Query().Get("code"))
	reply(w, out, err)
}
func sessionInput(w http.ResponseWriter, r *http.Request) (string, error) {
	var in struct {
		SessionID string `json:"session_id"`
	}
	err := decode(w, r, &in)
	if err == nil && (len(in.SessionID) < 32 || len(in.SessionID) > 100) {
		err = ErrInvalid
	}
	return in.SessionID, err
}
func (s *Service) pollHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := sessionInput(w, r)
	if err != nil {
		reply(w, nil, err)
		return
	}
	out, err := s.Poll(r.Context(), id)
	if len(out.Scopes) > 0 {
		out.Scopes = protocolScopes(r, out.Scopes)
	}
	reply(w, out, err)
}
func (s *Service) decideHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := s.browser(w, r)
	if !ok {
		return
	}
	id, err := sessionInput(w, r)
	if err != nil {
		reply(w, nil, err)
		return
	}
	out, err := s.Decide(r.Context(), u.ID, id, strings.HasSuffix(r.URL.Path, "/approve"))
	if scopes, ok := out["scopes"].([]string); ok {
		out["scopes"] = protocolScopes(r, scopes)
	}
	reply(w, out, err)
}
func (s *Service) devicesHTTP(w http.ResponseWriter, r *http.Request) {
	var uid int64
	if strings.Contains(r.URL.Path, "/authorized-devices") {
		d, ok := s.device(w, r, "account:read")
		if !ok {
			return
		}
		uid = d.UserID
	} else {
		u, ok := s.browser(w, r)
		if !ok {
			return
		}
		uid = u.ID
	}
	if r.Method == "DELETE" {
		reply(w, nil, s.Revoke(r.Context(), uid, positiveID(r.PathValue("id"))))
		return
	}
	out, err := s.Devices(r.Context(), uid)
	for i := range out {
		out[i].Scopes = protocolScopes(r, out[i].Scopes)
	}
	reply(w, out, err)
}
