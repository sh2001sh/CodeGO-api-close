package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// RetryError contains only a safe status code and server-requested backoff.
// Token endpoint bodies must never be included in errors or logs.
type RetryError struct {
	Status int
	After  time.Duration
}

func (e *RetryError) Error() string {
	return fmt.Sprintf("credentials: token endpoint status %d", e.Status)
}

type OAuthConfig struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scope        string
	JSONBody     bool // Claude OAuth expects JSON; Codex and Google use form encoding.
}

type OAuthRefresher struct {
	cfg        OAuthConfig
	transports *TransportPool
}

func NewOAuthRefresher(cfg OAuthConfig, transports *TransportPool) (*OAuthRefresher, error) {
	u, err := url.Parse(cfg.TokenURL)
	if err != nil {
		return nil, errors.New("credentials: invalid token endpoint")
	}
	localHTTP := u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")
	if u.Host == "" || u.User != nil || u.Fragment != "" ||
		(u.Scheme != "https" && !localHTTP) {
		return nil, errors.New("credentials: token endpoint must use HTTPS")
	}
	if cfg.ClientID == "" || transports == nil {
		return nil, errors.New("credentials: client ID and transport pool are required")
	}
	return &OAuthRefresher{cfg: cfg, transports: transports}, nil
}

// DefaultRefreshers supplies public Codex and Claude client IDs. Gemini CLI
// requires its configured client ID and secret rather than hard-coded secrets.
func DefaultRefreshers(transports *TransportPool, gemini OAuthConfig) (map[string]Refresher, error) {
	configs := map[string]OAuthConfig{
		"codex":     {TokenURL: "https://auth.openai.com/oauth/token", ClientID: "app_EMoamEEZ73f0CkXaXp7hrann"},
		"anthropic": {TokenURL: "https://platform.claude.com/v1/oauth/token", ClientID: "9d1c250a-e61b-44d9-88ed-5944d1962f5e", JSONBody: true},
	}
	if gemini.ClientID != "" {
		if gemini.TokenURL == "" {
			gemini.TokenURL = "https://oauth2.googleapis.com/token"
		}
		configs["gemini"] = gemini
	}
	result := make(map[string]Refresher, len(configs))
	for provider, cfg := range configs {
		r, err := NewOAuthRefresher(cfg, transports)
		if err != nil {
			return nil, err
		}
		result[provider] = r
	}
	return result, nil
}

// oauthRefreshToken extracts and validates the refresh_token field from the
// decoded OAuth credential document.
func oauthRefreshToken(document map[string]json.RawMessage) (string, error) {
	var refreshToken string
	if err := json.Unmarshal(document["refresh_token"], &refreshToken); err != nil || strings.TrimSpace(refreshToken) == "" {
		return "", errors.New("credentials: refresh token missing")
	}
	return refreshToken, nil
}

// buildOAuthTokenRequest builds the token-endpoint POST request for a
// refresh_token grant, encoding the body as JSON or form data per cfg.JSONBody.
func (r *OAuthRefresher) buildOAuthTokenRequest(ctx context.Context, refreshToken string) (*http.Request, error) {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {r.cfg.ClientID}}
	if r.cfg.ClientSecret != "" {
		form.Set("client_secret", r.cfg.ClientSecret)
	}
	if r.cfg.Scope != "" {
		form.Set("scope", r.cfg.Scope)
	}
	body := form.Encode()
	contentType := "application/x-www-form-urlencoded"
	if r.cfg.JSONBody {
		fields := make(map[string]string, len(form))
		for name, values := range form {
			fields[name] = values[0]
		}
		encoded, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		body = string(encoded)
		contentType = "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.TokenURL, strings.NewReader(body))
	if err != nil {
		return nil, errors.New("credentials: cannot construct token request")
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// oauthTokenResponse is the subset of a token endpoint's JSON response used
// to update a Credential.
type oauthTokenResponse struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	ID      string `json:"id_token"`
	Expires int64  `json:"expires_in"`
}

// doOAuthTokenRequest sends req, enforcing the retry-after contract on
// non-200 responses and the body size limit, and decodes the token response.
func doOAuthTokenRequest(ctx context.Context, client *http.Client, req *http.Request) (oauthTokenResponse, error) {
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return oauthTokenResponse{}, ctx.Err()
		}
		return oauthTokenResponse{}, errors.New("credentials: token endpoint transport failure")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return oauthTokenResponse{}, &RetryError{Status: resp.StatusCode, After: retryAfter(resp.Header.Get("Retry-After"))}
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(payload) > 1<<20 {
		return oauthTokenResponse{}, errors.New("credentials: token response unreadable or too large")
	}
	var tokens oauthTokenResponse
	if err = json.Unmarshal(payload, &tokens); err != nil || tokens.Access == "" || tokens.Expires <= 0 || tokens.Expires > int64((365*24*time.Hour)/time.Second) {
		return oauthTokenResponse{}, errors.New("credentials: invalid token response")
	}
	return tokens, nil
}

// applyOAuthTokens writes the refreshed tokens into c and its decoded
// document, returning the re-encoded Credential secret.
func applyOAuthTokens(c Credential, document map[string]json.RawMessage, tokens oauthTokenResponse) (Credential, error) {
	now := time.Now().UTC()
	c.ExpiresAt = now.Add(time.Duration(tokens.Expires) * time.Second)
	set := func(name, value string) { encoded, _ := json.Marshal(value); document[name] = encoded }
	set("access_token", tokens.Access)
	if tokens.Refresh != "" {
		set("refresh_token", tokens.Refresh)
	}
	if tokens.ID != "" {
		set("id_token", tokens.ID)
	}
	set("expired", c.ExpiresAt.Format(time.RFC3339))
	set("last_refresh", now.Format(time.RFC3339))
	document["expires_at"] = json.RawMessage(strconv.AppendInt(nil, c.ExpiresAt.Unix(), 10))
	var err error
	c.Secret, err = json.Marshal(document)
	if err != nil {
		return Credential{}, errors.New("credentials: cannot encode credential document")
	}
	return c, nil
}

func (r *OAuthRefresher) Refresh(ctx context.Context, c Credential) (Credential, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(c.Secret, &document); err != nil || document == nil {
		return Credential{}, errors.New("credentials: invalid OAuth credential document")
	}
	refreshToken, err := oauthRefreshToken(document)
	if err != nil {
		return Credential{}, err
	}
	req, err := r.buildOAuthTokenRequest(ctx, refreshToken)
	if err != nil {
		return Credential{}, err
	}
	client, fp, err := r.transports.Client(c.ID, c.ProxyURL, c.Fingerprint)
	if err != nil {
		return Credential{}, err
	}
	tokens, err := doOAuthTokenRequest(ctx, client, req)
	if err != nil {
		return Credential{}, err
	}
	c, err = applyOAuthTokens(c, document, tokens)
	if err != nil {
		return Credential{}, err
	}
	c.Fingerprint = fp
	return c, nil
}

func retryAfter(value string) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds > 86400 {
			seconds = 86400
		}
		return time.Duration(seconds) * time.Second
	}
	if until, err := http.ParseTime(value); err == nil {
		d := time.Until(until)
		if d > 24*time.Hour {
			d = 24 * time.Hour
		}
		if d > 0 {
			return d
		}
	}
	return 0
}
