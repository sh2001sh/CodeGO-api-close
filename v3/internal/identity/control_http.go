package identity

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/legacy/identitydto"
)

func (c *Control) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/user/register", c.registerHTTP)
	mux.HandleFunc("POST /api/user/login", c.loginHTTP)
	mux.HandleFunc("POST /api/user/refresh", c.refreshHTTP)
	mux.HandleFunc("POST /api/user/logout", c.logoutHTTP)
	mux.HandleFunc("GET /api/user/logout", c.logoutHTTP)
	mux.HandleFunc("GET /api/user/self", c.selfHTTP)
	mux.HandleFunc("POST /api/user/aff_transfer", c.affiliateTransferHTTP)
	mux.HandleFunc("GET /api/user/groups", c.groupsHTTP)
	mux.HandleFunc("GET /api/user/self/groups", c.groupsHTTP)
	mux.HandleFunc("PUT /api/user/self", c.updateProfileHTTP)
	mux.HandleFunc("GET /api/user/self/settings", c.settingsHTTP)
	mux.HandleFunc("PUT /api/user/self/settings", c.settingsHTTP)
	mux.HandleFunc("GET /api/user/{$}", c.usersHTTP)
	mux.HandleFunc("POST /api/user/{$}", c.createUserHTTP)
	mux.HandleFunc("PUT /api/user/{$}", c.updateUserBodyHTTP)
	mux.HandleFunc("GET /api/user/{id}", c.getUserHTTP)
	mux.HandleFunc("DELETE /api/user/{id}", c.deleteUserHTTP)
	mux.HandleFunc("PUT /api/user/{id}", c.updateUserHTTP)
	mux.HandleFunc("GET /api/token/{$}", c.keysHTTP)
	mux.HandleFunc("POST /api/token/{$}", c.createKeyHTTP)
	mux.HandleFunc("PUT /api/token/{$}", c.updateKeyHTTP)
	mux.HandleFunc("GET /api/token/{id}/key", c.revealKeyHTTP)
	mux.HandleFunc("POST /api/token/{id}/key", c.revealKeyHTTP)
	mux.HandleFunc("GET /api/token/{id}", c.keyHTTP)
	mux.HandleFunc("DELETE /api/token/{id}", c.deleteKeyHTTP)
	mux.HandleFunc("GET /api/oauth/{provider}", c.beginOAuthHTTP)
	mux.HandleFunc("GET /api/oauth/providers", c.oauthProvidersHTTP)
	mux.HandleFunc("GET /api/oauth/{provider}/callback", c.finishOAuthHTTP)
	mux.HandleFunc("POST /api/passkey/register/begin", c.passkeyRegistrationBegin)
	mux.HandleFunc("POST /api/passkey/register/finish", c.passkeyRegistrationFinish)
	mux.HandleFunc("POST /api/passkey/login/begin", c.passkeyLoginBegin)
	mux.HandleFunc("POST /api/passkey/login/finish", c.passkeyLoginFinish)
	mux.HandleFunc("GET /api/passkey", c.passkeyStatus)
	mux.HandleFunc("GET /api/user/passkey", c.passkeyStatus)
	mux.HandleFunc("POST /api/user/passkey/register/begin", c.passkeyRegistrationBegin)
	mux.HandleFunc("POST /api/user/passkey/register/finish", c.passkeyRegistrationFinish)
	mux.HandleFunc("POST /api/user/passkey/login/begin", c.passkeyLoginBegin)
	mux.HandleFunc("POST /api/user/passkey/login/finish", c.passkeyLoginFinish)
	mux.HandleFunc("POST /api/user/passkey/verify/begin", c.passkeyVerifyBegin)
	mux.HandleFunc("POST /api/user/passkey/verify/finish", c.passkeyVerifyFinish)
	mux.HandleFunc("DELETE /api/user/passkey", c.passkeyDeleteHTTP)
	return identitydto.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != "GET" && r.Method != "HEAD" && !c.sameOrigin(r) {
			c.reply(w, nil, ErrForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	}))
}

func (c *Control) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if c.cfg.PublicURL != "" {
		expected, err := url.Parse(c.cfg.PublicURL)
		return err == nil && u.Scheme == expected.Scheme && u.Host == expected.Host
	}
	return u.Host == r.Host
}

func requestToken(r *http.Request) string {
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}
	if cookie, err := r.Cookie("codego_session"); err == nil {
		return cookie.Value
	}
	return ""
}

func (c *Control) AuthenticateRequest(r *http.Request) (User, error) {
	return c.Authenticate(r.Context(), requestToken(r))
}

func (c *Control) requireUser(w http.ResponseWriter, r *http.Request) (User, bool) {
	u, err := c.AuthenticateRequest(r)
	if err != nil {
		c.reply(w, nil, err)
		return User{}, false
	}
	return u, true
}

func (c *Control) reply(w http.ResponseWriter, data any, err error) {
	status, message := http.StatusOK, ""
	if err != nil {
		var denied *oauthPolicyDenial
		switch {
		case errors.Is(err, ErrCredentials):
			status, message = http.StatusUnauthorized, "身份验证失败"
		case errors.Is(err, ErrForbidden):
			status, message = http.StatusForbidden, "无权执行此操作"
			if errors.As(err, &denied) && denied.message != "" {
				message = denied.message
			}
		case errors.Is(err, ErrInvalidInput):
			status, message = http.StatusBadRequest, "参数无效"
		case errors.Is(err, ErrNotFound):
			status, message = http.StatusNotFound, "记录不存在"
		case errors.Is(err, ErrDuplicate):
			status, message = http.StatusConflict, "记录已存在"
		case errors.Is(err, ErrAffiliateFunds):
			status, message = http.StatusConflict, "推广余额不足"
		default:
			status, message = http.StatusServiceUnavailable, "服务暂时不可用"
			c.log.Error("identity control request failed", "err", err)
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    any    `json:"data,omitempty"`
	}{err == nil, message, data})
}

func decodeControl(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		return ErrInvalidInput
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrInvalidInput
	}
	return nil
}

func (c *Control) allowLogin(w http.ResponseWriter, r *http.Request) bool {
	address, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		address = r.RemoteAddr
	}
	if !c.limiter.allow(address) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"success":false,"message":"请求过于频繁"}`))
		return false
	}
	return true
}
