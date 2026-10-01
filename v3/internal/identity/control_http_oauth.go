package identity

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (c *Control) beginOAuthHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Has("state") {
		c.finishOAuthHTTP(w, r)
		return
	}
	if !c.allowLogin(w, r) {
		return
	}
	var uid *int64
	if r.URL.Query().Get("bind") == "true" {
		u, ok := c.requireUser(w, r)
		if !ok {
			return
		}
		uid = &u.ID
	}
	target, state, err := c.BeginOAuth(r.Context(), r.PathValue("provider"), uid)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "codego_oauth_state", Value: state, Path: "/api/oauth", HttpOnly: true, Secure: strings.HasPrefix(c.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, Expires: c.cfg.Now().Add(5 * time.Minute)})
	returnTo := localOAuthReturnTo(r.URL.Query().Get("returnTo"))
	returnCookie := &http.Cookie{Name: "codego_oauth_return", Value: url.QueryEscape(returnTo), Path: "/api/oauth", HttpOnly: true, Secure: strings.HasPrefix(c.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, Expires: c.cfg.Now().Add(5 * time.Minute)}
	if returnTo == "" {
		returnCookie.MaxAge = -1
	}
	http.SetCookie(w, returnCookie)
	http.Redirect(w, r, target, http.StatusFound)
}

func (c *Control) finishOAuthHTTP(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie("codego_oauth_state")
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		c.reply(w, nil, ErrCredentials)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "codego_oauth_state", Path: "/api/oauth", Value: "", HttpOnly: true, MaxAge: -1})
	returnTo := "/dashboard"
	if cookie, err := r.Cookie("codego_oauth_return"); err == nil {
		decoded, decodeErr := url.QueryUnescape(cookie.Value)
		if decodeErr == nil {
			if target := localOAuthReturnTo(decoded); target != "" {
				returnTo = target
			}
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "codego_oauth_return", Path: "/api/oauth", Value: "", HttpOnly: true, MaxAge: -1})
	u, err := c.FinishOAuth(r.Context(), r.PathValue("provider"), state, r.URL.Query().Get("code"))
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	s, err := c.NewSession(r.Context(), u)
	if err == nil {
		c.setSession(w, s)
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, returnTo, http.StatusSeeOther)
			return
		}
	}
	c.reply(w, s, err)
}

func localOAuthReturnTo(raw string) string {
	if len(raw) > 4096 || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return ""
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.IsAbs() || u.Host != "" {
		return ""
	}
	// Check the decoded path too: browsers normalize backslashes, and an
	// encoded authority must never become a cross-origin login redirect.
	if !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || strings.ContainsAny(u.Path, "\\\r\n\x00") {
		return ""
	}
	return u.String()
}
