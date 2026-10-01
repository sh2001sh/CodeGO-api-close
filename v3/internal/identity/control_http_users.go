package identity

import (
	"net/http"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/legacy/identitydto"
)

func (c *Control) setSession(w http.ResponseWriter, s Session) {
	secure := len(c.cfg.PublicURL) >= 8 && c.cfg.PublicURL[:8] == "https://"
	http.SetCookie(w, &http.Cookie{Name: "codego_session", Value: s.AccessToken, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, Expires: s.ExpiresAt})
	http.SetCookie(w, &http.Cookie{Name: "codego_refresh", Value: s.RefreshToken, Path: "/api/user", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, Expires: c.cfg.Now().Add(c.cfg.RefreshTTL)})
}

func (c *Control) registerHTTP(w http.ResponseWriter, r *http.Request) {
	if !c.allowLogin(w, r) {
		return
	}
	var in RegisterInput
	if err := decodeControl(w, r, &in); err != nil {
		c.reply(w, nil, err)
		return
	}
	u, err := c.Register(r.Context(), in)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	s, err := c.NewSession(r.Context(), u)
	if err == nil {
		c.setSession(w, s)
	}
	c.reply(w, s, err)
}

func (c *Control) loginHTTP(w http.ResponseWriter, r *http.Request) {
	if !c.allowLogin(w, r) {
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeControl(w, r, &in); err != nil {
		c.reply(w, nil, err)
		return
	}
	if len(in.Username) > 100 || len(in.Password) > 72 {
		c.reply(w, nil, ErrCredentials)
		return
	}
	u, err := c.Login(r.Context(), in.Username, in.Password)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	s, err := c.NewSession(r.Context(), u)
	if err == nil {
		c.setSession(w, s)
	}
	c.reply(w, s, err)
}

func (c *Control) refreshHTTP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if r.ContentLength != 0 {
		if err := decodeControl(w, r, &in); err != nil {
			c.reply(w, nil, err)
			return
		}
	}
	if in.RefreshToken == "" {
		if cookie, err := r.Cookie("codego_refresh"); err == nil {
			in.RefreshToken = cookie.Value
		}
	}
	s, err := c.Refresh(r.Context(), in.RefreshToken)
	if err == nil {
		c.setSession(w, s)
	}
	c.reply(w, s, err)
}

func (c *Control) logoutHTTP(w http.ResponseWriter, r *http.Request) {
	if !c.sameOrigin(r) {
		c.reply(w, nil, ErrForbidden)
		return
	}
	err := c.Logout(r.Context(), requestToken(r))
	for _, cookie := range []struct{ name, path string }{{"codego_session", "/"}, {"codego_refresh", "/api/user"}} {
		http.SetCookie(w, &http.Cookie{Name: cookie.name, Value: "", Path: cookie.path, HttpOnly: true, MaxAge: -1, Expires: time.Unix(1, 0)})
	}
	c.reply(w, nil, err)
}

func (c *Control) selfHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if ok {
		c.reply(w, u, nil)
	}
}
func (c *Control) usersHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	if !identitydto.IsV3(r) {
		page, limit := legacyPagination(r)
		data, err := c.userPage(r.Context(), u, page, limit)
		c.reply(w, data, err)
		return
	}
	users, err := c.ListUsers(r.Context(), u, parseID(r.URL.Query().Get("before")), int(parseID(r.URL.Query().Get("page_size"))))
	c.reply(w, users, err)
}
func (c *Control) updateUserHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	var in UserUpdate
	if err := decodeControl(w, r, &in); err != nil {
		c.reply(w, nil, err)
		return
	}
	updated, err := c.UpdateUser(r.Context(), u, parseID(r.PathValue("id")), in)
	c.reply(w, updated, err)
}
