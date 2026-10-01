package identity

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

type OAuthProviderInfo struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

func (c *Control) oauthProvider(ctx context.Context, name string) (OAuthProvider, error) {
	if p, ok := c.cfg.OAuth[name]; ok {
		return p, nil
	}
	var p OAuthProvider
	var encrypted []byte
	var scopes string
	var enabled bool
	err := c.pool.QueryRow(ctx, `SELECT enabled,client_id,secret_ciphertext,authorization_endpoint,token_endpoint,user_info_endpoint,scopes,
	 user_id_field,username_field,display_name_field,email_field,auth_style,access_policy,access_denied_message
	 FROM v3_identity.oauth_providers WHERE slug=$1`, name).Scan(&enabled, &p.ClientID, &encrypted, &p.AuthorizationURL, &p.TokenURL, &p.UserInfoURL, &scopes,
		&p.UserIDField, &p.UsernameField, &p.DisplayNameField, &p.EmailField, &p.AuthStyle, &p.AccessPolicy, &p.AccessDeniedMessage)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.builtinOAuthProvider(ctx, name)
	}
	if err != nil {
		return p, err
	}
	if !enabled {
		return p, ErrNotFound
	}
	p.ClientSecret, err = c.decryptKey(encrypted)
	if err != nil {
		return OAuthProvider{}, errors.New("identity: cannot decrypt OAuth provider secret")
	}
	p.Scopes = strings.Fields(scopes)
	// Keep upstream registrations usable after the cut. The app's anonymous
	// /oauth/{provider} page forwards the callback to the state-bound API.
	p.RedirectURL = strings.TrimRight(c.cfg.PublicURL, "/") + "/oauth/" + url.PathEscape(name)
	return p, nil
}

func (c *Control) OAuthProviders(ctx context.Context) ([]OAuthProviderInfo, error) {
	rows, err := c.pool.Query(ctx, `SELECT slug,name,icon,enabled FROM v3_identity.oauth_providers ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	providers := []OAuthProviderInfo{}
	saved := map[string]bool{}
	for rows.Next() {
		var p OAuthProviderInfo
		var enabled bool
		if err := rows.Scan(&p.Slug, &p.Name, &p.Icon, &enabled); err != nil {
			return nil, err
		}
		saved[p.Slug] = true
		if enabled {
			providers = append(providers, p)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	builtin, err := c.builtinOAuthProviders(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range builtin {
		if !saved[p.Slug] {
			providers = append(providers, p)
		}
	}
	for name := range c.cfg.OAuth {
		if !slices.ContainsFunc(providers, func(p OAuthProviderInfo) bool { return p.Slug == name }) {
			providers = append(providers, OAuthProviderInfo{Slug: name, Name: name})
		}
	}
	slices.SortFunc(providers, func(a, b OAuthProviderInfo) int { return strings.Compare(a.Slug, b.Slug) })
	return providers, nil
}

func (c *Control) oauthProvidersHTTP(w http.ResponseWriter, r *http.Request) {
	providers, err := c.OAuthProviders(r.Context())
	c.reply(w, providers, err)
}
