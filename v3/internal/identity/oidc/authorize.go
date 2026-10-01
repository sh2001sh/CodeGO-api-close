package oidc

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(r.URL.RawQuery) > 8192 {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if q.Get("client_id") != s.cfg.ClientID || q.Get("redirect_uri") != s.cfg.RedirectURI {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	for _, values := range q {
		if len(values) != 1 {
			oauthError(w, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	if q.Get("response_type") != "code" || q.Get("state") == "" || len(q.Get("state")) > 512 ||
		len(q.Get("nonce")) > 256 || q.Get("code_challenge_method") != "S256" || !validChallenge(q.Get("code_challenge")) {
		s.authorizeError(w, r, q, "invalid_request")
		return
	}
	if !validScopes(q.Get("scope")) {
		s.authorizeError(w, r, q, "invalid_scope")
		return
	}
	uid, err := s.authenticate(r)
	if err != nil || uid <= 0 {
		// Build the return path ourselves, never from Host, forwarded headers or a caller-provided return URL.
		returnTo := "/api/oidc/authorize?" + q.Encode()
		http.Redirect(w, r, "/sign-in?returnTo="+url.QueryEscape(returnTo), http.StatusFound)
		return
	}
	code, err := s.issueCode(r.Context(), uid, q)
	if errors.Is(err, errGrant) {
		s.authorizeError(w, r, q, "access_denied")
		return
	}
	if err != nil {
		s.log.Error("oidc authorization failed", "error", err)
		s.authorizeError(w, r, q, "server_error")
		return
	}
	u, _ := url.Parse(s.cfg.RedirectURI)
	callback := u.Query()
	callback.Set("code", code)
	callback.Set("state", q.Get("state"))
	u.RawQuery = callback.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func validScopes(scope string) bool {
	if len(scope) > 256 || !hasScope(scope, "openid") {
		return false
	}
	for _, v := range strings.Fields(scope) {
		if v != "openid" && v != "profile" && v != "email" {
			return false
		}
	}
	return true
}

func (s *Server) authorizeError(w http.ResponseWriter, r *http.Request, q url.Values, code string) {
	u, _ := url.Parse(s.cfg.RedirectURI)
	callback := u.Query()
	callback.Set("error", code)
	if state := q.Get("state"); state != "" && len(state) <= 512 {
		callback.Set("state", state)
	}
	u.RawQuery = callback.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (s *Server) issueCode(ctx context.Context, uid int64, q url.Values) (string, error) {
	code, err := randomToken()
	if err != nil {
		return "", err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	err = tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR SHARE`, uid).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errGrant
	}
	if err != nil {
		return "", err
	}
	now := s.cfg.Now()
	// Expiry indexes bound routine cleanup to expired grants and tokens.
	if _, err = tx.Exec(ctx, `DELETE FROM v3_identity.oidc_codes WHERE expires_at<=$1`, now); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM v3_identity.oidc_tokens WHERE expires_at<=$1`, now); err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_identity.oidc_codes(code_hash,client_id,user_id,redirect_uri,scope,nonce,code_challenge,expires_at)
	 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, digest(code), s.cfg.ClientID, uid, s.cfg.RedirectURI,
		strings.Join(strings.Fields(q.Get("scope")), " "), q.Get("nonce"), q.Get("code_challenge"), now.Add(codeTTL))
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return code, nil
}
