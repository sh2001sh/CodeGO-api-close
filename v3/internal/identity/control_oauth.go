package identity

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// OAuthProvider endpoints are configured by an operator, never by a caller.
type OAuthProvider struct {
	ClientID            string   `json:"client_id"`
	ClientSecret        string   `json:"client_secret"`
	AuthorizationURL    string   `json:"authorization_url"`
	TokenURL            string   `json:"token_url"`
	UserInfoURL         string   `json:"user_info_url"`
	RedirectURL         string   `json:"redirect_url"`
	Scopes              []string `json:"scopes"`
	UserIDField         string   `json:"user_id_field"`
	UsernameField       string   `json:"username_field"`
	DisplayNameField    string   `json:"display_name_field"`
	EmailField          string   `json:"email_field"`
	AuthStyle           int      `json:"auth_style"`
	AccessPolicy        string   `json:"access_policy"`
	AccessDeniedMessage string   `json:"access_denied_message"`
}

func (c *Control) BeginOAuth(ctx context.Context, provider string, userID *int64) (string, string, error) {
	p, err := c.oauthProvider(ctx, provider)
	if err != nil {
		return "", "", err
	}
	if p.AuthStyle < 0 || p.AuthStyle > 2 {
		return "", "", ErrInvalidInput
	}
	for _, endpoint := range []string{p.AuthorizationURL, p.TokenURL, p.UserInfoURL, p.RedirectURL} {
		if !validBuiltinOAuthURL(endpoint) {
			return "", "", ErrInvalidInput
		}
	}
	state, err := randomToken()
	if err != nil {
		return "", "", err
	}
	verifier, err := randomToken()
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256([]byte(state))
	_, err = c.pool.Exec(ctx, `INSERT INTO v3_identity.oauth_challenges(state_hash,provider,verifier,user_id,expires_at) VALUES ($1,$2,$3,$4,$5)`, hash[:], provider, verifier, userID, c.cfg.Now().Add(5*time.Minute))
	if err != nil {
		return "", "", err
	}
	challenge := sha256.Sum256([]byte(verifier))
	u, _ := url.Parse(p.AuthorizationURL)
	q := u.Query()
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", p.RedirectURL)
	q.Set("response_type", "code")
	q.Set("state", state)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	q.Set("code_challenge_method", "S256")
	q.Set("scope", strings.Join(p.Scopes, " "))
	u.RawQuery = q.Encode()
	return u.String(), state, nil
}

func (c *Control) FinishOAuth(ctx context.Context, provider, state, code string) (User, error) {
	p, err := c.oauthProvider(ctx, provider)
	if err != nil {
		return User{}, err
	}
	if len(state) != 43 || code == "" || len(code) > 4096 {
		return User{}, ErrCredentials
	}
	verifier, userID, err := c.consumeOAuthChallenge(ctx, provider, state)
	if err != nil {
		return User{}, err
	}
	accessToken, err := c.exchangeOAuthCode(ctx, p, code, verifier)
	if err != nil {
		return User{}, err
	}
	info, err := c.fetchOAuthUserInfo(ctx, p, accessToken)
	if err != nil {
		return User{}, err
	}
	if err = checkOAuthPolicy(info, p.AccessPolicy); err != nil {
		var denied *oauthPolicyDenial
		if errors.As(err, &denied) {
			denied.message = renderOAuthDenial(p.AccessDeniedMessage, provider, info, denied)
		}
		return User{}, err
	}
	subject, name := oauthIdentity(info, p)
	if subject == "" || len(subject) > 512 {
		return User{}, ErrCredentials
	}
	return c.oauthUser(ctx, provider, subject, name, userID)
}

// consumeOAuthChallenge atomically redeems and deletes the PKCE challenge
// for state, returning the verifier and the user ID it was issued to link
// (nil for a fresh login/registration).
func (c *Control) consumeOAuthChallenge(ctx context.Context, provider, state string) (verifier string, userID *int64, err error) {
	hash := sha256.Sum256([]byte(state))
	err = c.pool.QueryRow(ctx, `DELETE FROM v3_identity.oauth_challenges WHERE state_hash=$1 AND provider=$2 AND expires_at>$3 RETURNING verifier,user_id`, hash[:], provider, c.cfg.Now()).Scan(&verifier, &userID)
	if err != nil {
		if errors.Is(controlDBError(err), ErrNotFound) {
			return "", nil, ErrCredentials
		}
		return "", nil, err
	}
	return verifier, userID, nil
}

// exchangeOAuthCode trades the authorization code and PKCE verifier for an
// access token at the provider's token endpoint.
func (c *Control) exchangeOAuthCode(ctx context.Context, p OAuthProvider, code, verifier string) (string, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {p.RedirectURL}, "client_id": {p.ClientID}, "client_secret": {p.ClientSecret}, "code_verifier": {verifier}}
	if p.AuthStyle == 2 {
		form.Del("client_id")
		form.Del("client_secret")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if p.AuthStyle == 2 {
		req.SetBasicAuth(p.ClientID, p.ClientSecret)
	}
	req.Header.Set("Accept", "application/json")
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err = c.oauthJSON(req, &token); err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		return "", ErrCredentials
	}
	return token.AccessToken, nil
}

// fetchOAuthUserInfo retrieves the raw user info document using accessToken.
func (c *Control) fetchOAuthUserInfo(ctx context.Context, p OAuthProvider, accessToken string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.UserInfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	var info json.RawMessage
	if err = c.oauthJSON(req, &info); err != nil {
		return nil, err
	}
	return info, nil
}

// oauthIdentity extracts the subject and display name from a provider's user
// info document, applying p's field overrides and the sub/id and name
// fallbacks.
func oauthIdentity(info json.RawMessage, p OAuthProvider) (subject, name string) {
	subjectField := p.UserIDField
	if subjectField == "" {
		subjectField = "sub"
	}
	subject = gjson.GetBytes(info, subjectField).String()
	if subject == "" && p.UserIDField == "" {
		subject = gjson.GetBytes(info, "id").String()
	}
	nameField := p.DisplayNameField
	if nameField == "" {
		nameField = "name"
	}
	name = gjson.GetBytes(info, nameField).String()
	return subject, name
}

func (c *Control) oauthJSON(req *http.Request, out any) error {
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("identity: oauth transport: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errors.New("identity: oauth provider rejected request")
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return errors.New("identity: invalid oauth provider response")
	}
	return nil
}

func (c *Control) oauthUser(ctx context.Context, provider, subject, name string, binding *int64) (User, error) {
	// v2 exposed the linuxdo route but persisted user bindings as linux_do.
	// Retain that namespace so imported users recover their existing account.
	if provider == "linuxdo" {
		provider = "linux_do"
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Identity-level locking serializes first login; it does not serialize
	// unrelated accounts or trust an email supplied by a provider.
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, provider+":"+subject)
	if err != nil {
		return User{}, err
	}
	var uid int64
	err = tx.QueryRow(ctx, `SELECT user_id FROM v3_identity.user_identities WHERE provider=$1 AND subject=$2`, provider, subject).Scan(&uid)
	if err != nil && !errors.Is(controlDBError(err), ErrNotFound) {
		return User{}, err
	}
	if err == nil && binding != nil && *binding != uid {
		return User{}, ErrDuplicate
	}
	if errors.Is(controlDBError(err), ErrNotFound) {
		if binding != nil {
			uid = *binding
		} else {
			if c.cfg.DisableRegistration {
				return User{}, ErrForbidden
			}
			random, err := randomToken()
			if err != nil {
				return User{}, err
			}
			if len(name) > 100 {
				name = ""
			}
			err = tx.QueryRow(ctx, `INSERT INTO v3_identity.users(username,display_name) VALUES ($1,$2) RETURNING id`, "oauth_"+random[:20], name).Scan(&uid)
			if err != nil {
				return User{}, err
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO v3_identity.user_identities(provider,subject,user_id) VALUES ($1,$2,$3)`, provider, subject, uid)
		if err != nil {
			return User{}, controlDBError(err)
		}
	}
	u, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL`, uid))
	if err != nil {
		return User{}, ErrCredentials
	}
	if err = tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return u, nil
}

func parseID(s string) int64 { id, _ := strconv.ParseInt(s, 10, 64); return id }
