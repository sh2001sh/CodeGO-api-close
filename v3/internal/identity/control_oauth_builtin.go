package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

var builtinOAuthNames = []string{"github", "linuxdo", "discord", "oidc"}

var builtinOAuthKeys = []string{
	"GitHubOAuthEnabled", "GitHubClientId", "GitHubClientSecret",
	"LinuxDOOAuthEnabled", "LinuxDOClientId", "LinuxDOClientSecret", "LinuxDOMinimumTrustLevel",
	"discord.enabled", "discord.client_id", "discord.client_secret",
	"oidc.enabled", "oidc.client_id", "oidc.client_secret", "oidc.authorization_endpoint", "oidc.token_endpoint", "oidc.user_info_endpoint",
	"ServerAddress",
}

func (c *Control) builtinOAuthProvider(ctx context.Context, name string) (OAuthProvider, error) {
	if !isBuiltinOAuth(name) {
		return OAuthProvider{}, ErrNotFound
	}
	if p, overridden := c.cfg.OAuth[name]; overridden {
		return p, nil
	}
	settings, err := c.loadBuiltinOAuthSettings(ctx)
	if err != nil {
		return OAuthProvider{}, err
	}
	return buildBuiltinOAuthProvider(name, c.cfg.PublicURL, settings)
}

func (c *Control) builtinOAuthProviders(ctx context.Context) ([]OAuthProviderInfo, error) {
	settings, err := c.loadBuiltinOAuthSettings(ctx)
	if err != nil {
		return nil, err
	}
	providers := []OAuthProviderInfo{}
	for _, name := range builtinOAuthNames {
		if _, overridden := c.cfg.OAuth[name]; overridden {
			continue
		}
		_, err := buildBuiltinOAuthProvider(name, c.cfg.PublicURL, settings)
		if err == ErrNotFound {
			continue
		}
		if err != nil {
			return nil, err
		}
		display := map[string]string{"github": "GitHub", "linuxdo": "Linux DO", "discord": "Discord", "oidc": "OIDC"}[name]
		providers = append(providers, OAuthProviderInfo{Slug: name, Name: display})
	}
	return providers, nil
}

func isBuiltinOAuth(name string) bool {
	for _, builtin := range builtinOAuthNames {
		if name == builtin {
			return true
		}
	}
	return false
}

func (c *Control) loadBuiltinOAuthSettings(ctx context.Context) (map[string]string, error) {
	keys := []string{}
	for _, key := range builtinOAuthKeys {
		for name, prefix := range map[string]string{"github": "GitHub", "linuxdo": "LinuxDO", "discord": "discord.", "oidc": "oidc."} {
			if strings.HasPrefix(key, prefix) {
				if _, overridden := c.cfg.OAuth[name]; !overridden {
					keys = append(keys, key)
				}
				break
			}
		}
	}
	if len(keys) == 0 {
		return map[string]string{}, nil
	}
	if c.cfg.PublicURL == "" {
		keys = append(keys, "ServerAddress")
	}
	rows, err := c.pool.Query(ctx, `SELECT key,value,ciphertext,sensitive FROM v3_platform.settings WHERE key=ANY($1::text[])`, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := map[string]string{}
	for rows.Next() {
		var key string
		var raw, encrypted []byte
		var sensitive bool
		if err := rows.Scan(&key, &raw, &encrypted, &sensitive); err != nil {
			return nil, err
		}
		if sensitive {
			plaintext, err := c.decryptKey(encrypted)
			if err != nil {
				return nil, fmt.Errorf("identity: cannot decrypt built-in OAuth setting %s", key)
			}
			raw = []byte(plaintext)
		}
		value, err := decodeBuiltinOAuthValue(raw)
		if err != nil {
			return nil, fmt.Errorf("identity: invalid built-in OAuth setting %s: %w", key, ErrInvalidInput)
		}
		settings[key] = value
	}
	return settings, rows.Err()
}

// Legacy options encode already-valid JSON scalars directly, including numeric
// client IDs. Keep their exact scalar spelling instead of using float64.
func decodeBuiltinOAuthValue(raw []byte) (string, error) {
	if !json.Valid(raw) {
		return "", ErrInvalidInput
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "null" {
		return "", ErrInvalidInput
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	if trimmed == "true" || trimmed == "false" {
		return trimmed, nil
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil && number.String() != "" && trimmed != "null" {
		return number.String(), nil
	}
	return "", ErrInvalidInput
}

func buildBuiltinOAuthProvider(name, publicURL string, settings map[string]string) (OAuthProvider, error) {
	p, enabledKey, idKey, secretKey, err := builtinOAuthTemplate(name, settings)
	if err != nil {
		return OAuthProvider{}, err
	}
	flag := settings[enabledKey]
	if flag == "" || flag == "false" {
		return OAuthProvider{}, ErrNotFound
	}
	if flag != "true" {
		return OAuthProvider{}, fmt.Errorf("identity: invalid built-in OAuth enabled flag %s: %w", enabledKey, ErrInvalidInput)
	}
	p.ClientID, p.ClientSecret = settings[idKey], settings[secretKey]
	if strings.TrimSpace(p.ClientID) == "" || strings.TrimSpace(p.ClientSecret) == "" {
		return OAuthProvider{}, fmt.Errorf("identity: incomplete built-in OAuth configuration %s: %w", name, ErrInvalidInput)
	}
	if err := p.resolveRedirectURL(name, publicURL, settings); err != nil {
		return OAuthProvider{}, err
	}
	for _, endpoint := range []string{p.AuthorizationURL, p.TokenURL, p.UserInfoURL, p.RedirectURL} {
		if !validBuiltinOAuthURL(endpoint) {
			return OAuthProvider{}, fmt.Errorf("identity: invalid built-in OAuth endpoint %s: %w", name, ErrInvalidInput)
		}
	}
	if name == "linuxdo" {
		if err := p.applyLinuxDOTrustPolicy(settings); err != nil {
			return OAuthProvider{}, err
		}
	}
	return p, nil
}

// builtinOAuthTemplate returns the fixed endpoint/scope/field template for a
// built-in provider, along with the setting keys that gate and credential it.
func builtinOAuthTemplate(name string, settings map[string]string) (p OAuthProvider, enabledKey, idKey, secretKey string, err error) {
	p = OAuthProvider{AuthStyle: 1}
	switch name {
	case "github":
		enabledKey, idKey, secretKey = "GitHubOAuthEnabled", "GitHubClientId", "GitHubClientSecret"
		p.AuthorizationURL, p.TokenURL, p.UserInfoURL = "https://github.com/login/oauth/authorize", "https://github.com/login/oauth/access_token", "https://api.github.com/user"
		p.Scopes, p.UserIDField, p.UsernameField, p.DisplayNameField, p.EmailField = []string{"user:email"}, "id", "login", "name", "email"
	case "linuxdo":
		enabledKey, idKey, secretKey = "LinuxDOOAuthEnabled", "LinuxDOClientId", "LinuxDOClientSecret"
		p.AuthorizationURL, p.TokenURL, p.UserInfoURL = "https://connect.linux.do/oauth2/authorize", "https://connect.linux.do/oauth2/token", "https://connect.linux.do/api/user"
		p.AuthStyle, p.UserIDField, p.UsernameField, p.DisplayNameField = 2, "id", "username", "name"
	case "discord":
		enabledKey, idKey, secretKey = "discord.enabled", "discord.client_id", "discord.client_secret"
		p.AuthorizationURL, p.TokenURL, p.UserInfoURL = "https://discord.com/oauth2/authorize", "https://discord.com/api/v10/oauth2/token", "https://discord.com/api/v10/users/@me"
		p.Scopes, p.UserIDField, p.UsernameField, p.DisplayNameField = []string{"identify", "openid"}, "id", "username", "global_name"
	case "oidc":
		enabledKey, idKey, secretKey = "oidc.enabled", "oidc.client_id", "oidc.client_secret"
		p.AuthorizationURL, p.TokenURL, p.UserInfoURL = settings["oidc.authorization_endpoint"], settings["oidc.token_endpoint"], settings["oidc.user_info_endpoint"]
		p.Scopes, p.UserIDField, p.UsernameField, p.DisplayNameField, p.EmailField = []string{"openid", "profile", "email"}, "sub", "preferred_username", "name", "email"
	default:
		return OAuthProvider{}, "", "", "", ErrNotFound
	}
	return p, enabledKey, idKey, secretKey, nil
}

// resolveRedirectURL determines the operator's public base URL and derives
// p's redirect URL from it.
func (p *OAuthProvider) resolveRedirectURL(name, publicURL string, settings map[string]string) error {
	if publicURL == "" {
		publicURL = settings["ServerAddress"]
	}
	base, err := url.Parse(publicURL)
	if err != nil || base.RawQuery != "" || !validBuiltinOAuthURL(publicURL) {
		return fmt.Errorf("identity: invalid built-in OAuth public URL: %w", ErrInvalidInput)
	}
	p.RedirectURL = strings.TrimRight(publicURL, "/") + "/oauth/" + name
	return nil
}

// applyLinuxDOTrustPolicy sets the access policy gating login by LinuxDO
// trust level.
func (p *OAuthProvider) applyLinuxDOTrustPolicy(settings map[string]string) error {
	minimum := 0
	if raw, ok := settings["LinuxDOMinimumTrustLevel"]; ok {
		var err error
		minimum, err = strconv.Atoi(raw)
		if err != nil || minimum < 0 {
			return fmt.Errorf("identity: invalid LinuxDO minimum trust level: %w", ErrInvalidInput)
		}
	}
	p.AccessPolicy = fmt.Sprintf(`{"logic":"and","conditions":[{"field":"trust_level","op":"gte","value":%d}]}`, minimum)
	p.AccessDeniedMessage = "Linux DO trust level must be at least {{required}}; your trust level is {{current}}."
	return nil
}

func validBuiltinOAuthURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	return u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")
}
