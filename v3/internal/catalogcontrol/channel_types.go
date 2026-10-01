package catalogcontrol

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

type Channel struct {
	ID                        int64             `json:"id"`
	Name                      string            `json:"name"`
	Provider                  string            `json:"provider"`
	BaseURL                   string            `json:"base_url"`
	ProxyURL                  string            `json:"proxy_url"`
	Status                    string            `json:"status"`
	Scope                     string            `json:"scope"`
	OwnerUserID               *int64            `json:"owner_user_id"`
	Priority                  int               `json:"priority"`
	Weight                    int               `json:"weight"`
	MaxConcurrency            int               `json:"max_concurrency"`
	MaxUserConcurrency        int               `json:"max_user_concurrency"`
	AutoDisable               bool              `json:"auto_disable"`
	MultiplierCardSupported   bool              `json:"multiplier_card_supported"`
	MultiplierCardUserEnabled *bool             `json:"multiplier_card_user_enabled,omitempty"`
	ModelMapping              map[string]string `json:"model_mapping"`
	ParamOverride             json.RawMessage   `json:"param_override"`
	HeaderOverride            map[string]string `json:"header_override"`
	StatusCodeMapping         json.RawMessage   `json:"status_code_mapping"`
	Settings                  json.RawMessage   `json:"settings"`
	Tag                       *string           `json:"tag"`
	Remark                    string            `json:"remark"`
	Groups                    []string          `json:"groups"`
	Models                    []string          `json:"models"`
	Credentials               []CredentialInput `json:"credentials,omitempty"`
	AppendCredentials         bool              `json:"append_credentials,omitempty"`
}

// CredentialInput is write-only. Channel reads never include secret values.
type CredentialInput struct {
	Secret         string                         `json:"secret"`
	Kind           string                         `json:"kind"`
	ExpiresAt      *time.Time                     `json:"expires_at,omitempty"`
	MaxConcurrency int                            `json:"max_concurrency,omitempty"`
	Fingerprint    *catalog.CredentialFingerprint `json:"fingerprint,omitempty"`
}

// validateChannelCore checks name/provider/status/scope/owner/limits.
func (c *Channel) validateChannelCore() error {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || len(c.Name) > 255 || c.Provider == "" || len(c.Provider) > 64 {
		return fmt.Errorf("name and provider are required")
	}
	if c.Status == "" {
		c.Status = "enabled"
	}
	if c.Scope == "" {
		c.Scope = "official"
	}
	if c.Status != "enabled" && c.Status != "disabled" && c.Status != "auto_disabled" {
		return fmt.Errorf("invalid status")
	}
	if c.Scope != "official" && c.Scope != "marketplace" {
		return fmt.Errorf("invalid scope")
	}
	if c.Scope == "marketplace" && (c.OwnerUserID == nil || *c.OwnerUserID <= 0) {
		return fmt.Errorf("marketplace channels require an owner")
	}
	if c.Weight < 0 || c.MaxConcurrency < 0 || c.MaxUserConcurrency < 0 {
		return fmt.Errorf("limits and weight must be non-negative")
	}
	return nil
}

// validateChannelURLs checks BaseURL and ProxyURL for valid, host-bearing,
// userinfo-free URLs with an allowed scheme (proxy additionally allows
// socks5/socks5h).
func (c *Channel) validateChannelURLs() error {
	for _, item := range []struct {
		value string
		proxy bool
	}{{c.BaseURL, false}, {c.ProxyURL, true}} {
		if item.value == "" {
			continue
		}
		u, err := url.Parse(item.value)
		if err != nil {
			return fmt.Errorf("invalid upstream or proxy URL")
		}
		validScheme := u.Scheme == "https" || u.Scheme == "http" || (item.proxy && (u.Scheme == "socks5" || u.Scheme == "socks5h"))
		if u.Host == "" || u.User != nil || !validScheme {
			return fmt.Errorf("invalid upstream or proxy URL")
		}
	}
	return nil
}

// validateChannelMemberships checks the Groups and Models lists for size and
// per-name constraints.
func (c *Channel) validateChannelMemberships() error {
	for _, list := range [][]string{c.Groups, c.Models} {
		if len(list) > 10000 {
			return fmt.Errorf("too many memberships")
		}
		for _, name := range list {
			if name == "" || len(name) > 255 {
				return fmt.Errorf("invalid group or model name")
			}
		}
	}
	return nil
}

// validateChannelCredentials checks and normalizes the Credentials list,
// defaulting Kind, deriving oauth expiry and validating fingerprints.
func (c *Channel) validateChannelCredentials() error {
	if len(c.Credentials) > 10000 {
		return fmt.Errorf("too many credentials")
	}
	for i := range c.Credentials {
		cr := &c.Credentials[i]
		if cr.Kind == "" {
			cr.Kind = "api_key"
		}
		if cr.MaxConcurrency < 0 || cr.Secret == "" || len(cr.Secret) > 131072 || (cr.Kind != "api_key" && cr.Kind != "oauth") {
			return fmt.Errorf("invalid credential secret or kind")
		}
		if cr.Kind == "oauth" && cr.ExpiresAt == nil {
			cr.ExpiresAt = oauthExpiry(cr.Secret)
		}
		if err := validateFingerprint(cr.Fingerprint); err != nil {
			return err
		}
	}
	return nil
}

// validateChannelConfig checks that ParamOverride/Settings/StatusCodeMapping
// are JSON objects (defaulting empty ones to `{}`), validates the status
// code mapping shape, and folds MultiplierCardUserEnabled into Settings.
func (c *Channel) validateChannelConfig() error {
	for _, raw := range []*json.RawMessage{&c.ParamOverride, &c.Settings, &c.StatusCodeMapping} {
		if len(*raw) == 0 {
			*raw = json.RawMessage(`{}`)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(*raw, &obj); err != nil || obj == nil {
			return fmt.Errorf("configuration values must be JSON objects")
		}
	}
	if _, err := catalog.ParseStatusCodeMapping(c.StatusCodeMapping); err != nil {
		return err
	}
	if c.MultiplierCardUserEnabled != nil {
		var settings map[string]json.RawMessage
		if err := json.Unmarshal(c.Settings, &settings); err != nil {
			return err
		}
		settings["multiplier_card_user_enabled"], _ = json.Marshal(*c.MultiplierCardUserEnabled)
		var err error
		c.Settings, err = json.Marshal(settings)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Channel) validate() error {
	if err := c.validateChannelCore(); err != nil {
		return err
	}
	if err := c.validateChannelURLs(); err != nil {
		return err
	}
	if err := c.validateChannelMemberships(); err != nil {
		return err
	}
	if err := c.validateChannelCredentials(); err != nil {
		return err
	}
	return c.validateChannelConfig()
}

func validateFingerprint(fp *catalog.CredentialFingerprint) error {
	if fp == nil {
		return nil
	}
	if (fp.TLSProfile != "" && fp.TLSProfile != "chrome" && fp.TLSProfile != "firefox") || len(fp.UserAgent) > 1024 {
		return fmt.Errorf("invalid credential fingerprint")
	}
	for _, character := range fp.UserAgent {
		if character < ' ' || character == 127 {
			return fmt.Errorf("invalid credential user agent")
		}
	}
	return nil
}

const channelSelect = `SELECT c.id,c.name,c.provider,c.base_url,c.proxy_url,c.status,c.scope,c.owner_user_id,
 c.priority,c.weight,c.max_concurrency,c.max_user_concurrency,c.auto_disable,c.multiplier_card_supported,
 c.model_mapping,c.param_override,c.header_override,c.status_code_mapping,c.settings,c.tag,c.remark,
 ARRAY(SELECT group_name FROM v3_catalog.channel_groups WHERE channel_id=c.id ORDER BY group_name),
 ARRAY(SELECT model FROM v3_catalog.channel_models WHERE channel_id=c.id ORDER BY model)
 FROM v3_catalog.channels c`

type scanner interface{ Scan(...any) error }

func scanChannel(row scanner) (Channel, error) {
	var c Channel
	err := row.Scan(&c.ID, &c.Name, &c.Provider, &c.BaseURL, &c.ProxyURL, &c.Status, &c.Scope, &c.OwnerUserID,
		&c.Priority, &c.Weight, &c.MaxConcurrency, &c.MaxUserConcurrency, &c.AutoDisable, &c.MultiplierCardSupported,
		&c.ModelMapping, &c.ParamOverride, &c.HeaderOverride, &c.StatusCodeMapping, &c.Settings, &c.Tag, &c.Remark, &c.Groups, &c.Models)
	if err == nil {
		var settings map[string]any
		if err = json.Unmarshal(c.Settings, &settings); err == nil {
			enabled := catalog.CardUserEnabled(settings, c.MultiplierCardSupported)
			c.MultiplierCardUserEnabled = &enabled
		}
	}
	return c, err
}
