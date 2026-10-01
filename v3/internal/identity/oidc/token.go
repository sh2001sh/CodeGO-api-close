package oidc

import (
	"context"
	"crypto/subtle"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
)

type tokenResult struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	IDToken     string `json:"id_token"`
	Scope       string `json:"scope"`
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" || r.URL.RawQuery != "" {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err = r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	for _, values := range r.PostForm {
		if len(values) != 1 {
			oauthError(w, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	id, secret, ok := s.clientCredentials(r)
	if !ok || id != s.cfg.ClientID || subtle.ConstantTimeCompare(digest(secret), digest(s.cfg.ClientSecret)) != 1 {
		w.Header().Set("WWW-Authenticate", `Basic realm="codego-oidc"`)
		oauthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	if r.PostForm.Get("grant_type") != "authorization_code" {
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	result, err := s.exchange(r.Context(), r.PostForm)
	if errors.Is(err, errGrant) {
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	if err != nil {
		s.log.Error("oidc token exchange failed", "error", err)
		oauthError(w, http.StatusInternalServerError, "server_error")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) clientCredentials(r *http.Request) (string, string, bool) {
	if r.Header.Get("Authorization") != "" {
		id, secret, ok := r.BasicAuth()
		if !ok || r.PostForm.Has("client_id") || r.PostForm.Has("client_secret") {
			return "", "", false
		}
		// RFC 6749 section 2.3.1 form-encodes each component before Basic encoding.
		id, err := url.QueryUnescape(id)
		if err != nil {
			return "", "", false
		}
		secret, err = url.QueryUnescape(secret)
		return id, secret, err == nil
	}
	return r.PostForm.Get("client_id"), r.PostForm.Get("client_secret"), true
}

func (s *Server) exchange(ctx context.Context, form url.Values) (tokenResult, error) {
	code, verifier := form.Get("code"), form.Get("code_verifier")
	if !validChallenge(code) || !validVerifier(verifier) || form.Get("redirect_uri") != s.cfg.RedirectURI {
		return tokenResult{}, errGrant
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return tokenResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := s.cfg.Now()
	uid, scope, nonce, err := s.consumeAuthorizationCodeTx(ctx, tx, code, verifier, now)
	if err != nil {
		return tokenResult{}, err
	}
	u, err := loadUser(ctx, tx, uid)
	if err != nil {
		return tokenResult{}, err
	}
	if u.Subject == "" {
		u.Subject, err = assignSubject(ctx, tx, uid)
		if err != nil {
			return tokenResult{}, err
		}
	}
	access, idToken, err := s.issueTokensTx(ctx, tx, u, uid, scope, nonce, now)
	if err != nil {
		return tokenResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return tokenResult{}, err
	}
	return tokenResult{AccessToken: access, TokenType: "Bearer", ExpiresIn: int64(tokenTTL.Seconds()), IDToken: idToken, Scope: strings.Join(strings.Fields(scope), " ")}, nil
}

// consumeAuthorizationCodeTx redeems and deletes the authorization code,
// validating the PKCE verifier against its stored challenge.
//
// DELETE locks and consumes exactly one grant. Any later failure rolls it
// back; concurrent exchanges cannot both commit token issuance.
func (s *Server) consumeAuthorizationCodeTx(ctx context.Context, tx pgx.Tx, code, verifier string, now time.Time) (uid int64, scope, nonce string, err error) {
	var challenge string
	err = tx.QueryRow(ctx, `DELETE FROM v3_identity.oidc_codes WHERE code_hash=$1 AND client_id=$2
	 AND redirect_uri=$3 AND expires_at>$4 RETURNING user_id,scope,nonce,code_challenge`, digest(code), s.cfg.ClientID,
		s.cfg.RedirectURI, now).Scan(&uid, &scope, &nonce, &challenge)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", "", errGrant
	}
	if err != nil {
		return 0, "", "", err
	}
	if !pkceMatches(verifier, challenge) {
		return 0, "", "", errGrant
	}
	return uid, scope, nonce, nil
}

// issueTokensTx records a new access token and signs the matching ID token.
func (s *Server) issueTokensTx(ctx context.Context, tx pgx.Tx, u user, uid int64, scope, nonce string, now time.Time) (access, idToken string, err error) {
	access, err = randomToken()
	if err != nil {
		return "", "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_identity.oidc_tokens(token_hash,client_id,user_id,scope,expires_at)
	 VALUES ($1,$2,$3,$4,$5)`, digest(access), s.cfg.ClientID, uid, scope, now.Add(tokenTTL))
	if err != nil {
		return "", "", err
	}
	claims := jwt.MapClaims{"iss": s.cfg.Issuer, "sub": u.Subject, "aud": s.cfg.ClientID, "iat": now.Unix(), "exp": now.Add(tokenTTL).Unix()}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	for name, value := range userClaims(u, scope) {
		claims[name] = value
	}
	id := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	id.Header["kid"] = s.cfg.KeyID
	idToken, err = id.SignedString(s.cfg.PrivateKey)
	if err != nil {
		return "", "", err
	}
	return access, idToken, nil
}
