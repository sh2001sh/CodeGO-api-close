package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type sourceChannel struct {
	ID              int64           `json:"id"`
	Type            int             `json:"type"`
	Name            string          `json:"name"`
	Key             string          `json:"key"`
	BaseURL         string          `json:"base_url"`
	ProxyURL        string          `json:"-"`
	Other           string          `json:"other"`
	Setting         string          `json:"setting"`
	Organization    *string         `json:"openai_organization"`
	Status          int             `json:"status"`
	Scope           string          `json:"channel_scope"`
	Group           string          `json:"group"`
	Models          string          `json:"models"`
	Priority        int64           `json:"priority"`
	Weight          int             `json:"weight"`
	Concurrency     int             `json:"marketplace_max_concurrency"`
	UserConcurrency int             `json:"marketplace_user_max_concurrency"`
	CardSupported   bool            `json:"multiplier_card_supported"`
	CardUserEnabled *bool           `json:"multiplier_card_user_enabled"`
	ModelMapping    string          `json:"model_mapping"`
	ParamOverride   string          `json:"param_override"`
	HeaderOverride  string          `json:"header_override"`
	StatusMapping   string          `json:"status_code_mapping"`
	Settings        string          `json:"settings"`
	Tag             string          `json:"tag"`
	Remark          string          `json:"remark"`
	AutoBan         *int            `json:"auto_ban"`
	ChannelInfo     json.RawMessage `json:"channel_info"`
}

// Provider IDs match v2's published adapter IDs; no unknown adapter is silently
// coerced to OpenAI. The gateway's registry must contain the imported provider.
func providerID(kind int) (string, error) {
	ids := map[int]string{1: "openai", 3: "azure", 4: "ollama", 6: "openai_max", 7: "ohmygpt", 8: "custom", 9: "ails", 10: "aiproxy", 11: "palm", 12: "api2gpt", 13: "aigc2d", 14: "anthropic", 15: "baidu", 16: "zhipu", 17: "ali", 18: "xunfei", 19: "360", 20: "openrouter", 21: "aiproxy_library", 22: "fastgpt", 23: "tencent", 24: "gemini", 25: "moonshot", 26: "zhipu_4v", 27: "perplexity", 31: "lingyiwanwu", 33: "aws", 34: "cohere", 35: "minimax", 36: "suno", 37: "dify", 38: "jina", 39: "cloudflare", 40: "siliconflow", 41: "vertex", 42: "mistral", 43: "deepseek", 44: "mokaai", 45: "volcengine", 46: "baidu_v2", 47: "xinference", 48: "xai", 49: "coze", 50: "kling", 51: "jimeng", 52: "vidu", 53: "submodel", 54: "doubao_video", 56: "replicate", 57: "codex"}
	if provider := ids[kind]; provider != "" {
		return provider, nil
	}
	return "", fmt.Errorf("legacy: unsupported channel type %d", kind)
}

func decodeChannel(row json.RawMessage) (sourceChannel, string, error) {
	var c sourceChannel
	if err := json.Unmarshal(row, &c); err != nil {
		return c, "", err
	}
	provider, err := providerID(c.Type)
	if err != nil {
		return c, "", err
	}
	if c.BaseURL == "" {
		defaults, defaultErr := pricingDefaults()
		if defaultErr != nil {
			return c, "", defaultErr
		}
		c.BaseURL = defaults["ProviderBaseURLs"][strconv.Itoa(c.Type)]
	}
	parsed, parseErr := url.Parse(c.BaseURL)
	if parseErr != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return c, "", fmt.Errorf("legacy: channel %d has no valid upstream URL", c.ID)
	}
	if c.ID <= 0 || c.Name == "" || c.Weight < 0 || c.Concurrency < 0 || c.UserConcurrency < 0 || c.Priority < -2147483648 || c.Priority > 2147483647 {
		return c, "", fmt.Errorf("legacy: channel %d has invalid routing fields", c.ID)
	}
	for _, raw := range []string{c.ModelMapping, c.ParamOverride, c.HeaderOverride, c.StatusMapping, c.Settings, c.Setting} {
		if raw != "" && !json.Valid([]byte(raw)) {
			return c, "", fmt.Errorf("legacy: channel %d has invalid JSON settings", c.ID)
		}
	}
	if err := channelConfiguration(&c); err != nil {
		return c, "", fmt.Errorf("legacy: channel %d: %w", c.ID, err)
	}
	if len(list(c.Key)) == 0 {
		return c, "", fmt.Errorf("legacy: channel %d has no credentials", c.ID)
	}
	for index, secret := range splitSecrets(c.Key) {
		if _, _, _, err := credentialProperties(secret, c.ChannelInfo, index); err != nil {
			return c, "", fmt.Errorf("legacy: channel %d credential %d: %w", c.ID, index, err)
		}
	}
	return c, provider, nil
}

func jsonObject(value string) string {
	if strings.TrimSpace(value) == "" {
		return "{}"
	}
	return value
}

func (m *Importer) importChannels(ctx context.Context, tx pgx.Tx, rows []json.RawMessage) error {
	for _, row := range rows {
		c, provider, err := decodeChannel(row)
		if err != nil {
			return err
		}
		status, scope := "enabled", "official"
		if c.Status == 2 {
			status = "disabled"
		} else if c.Status != 1 {
			status = "auto_disabled"
		}
		if c.Scope == "external" {
			scope = "marketplace"
		}
		autoDisable := c.AutoBan == nil || *c.AutoBan != 0
		tag, err := tx.Exec(ctx, `INSERT INTO v3_catalog.channels
			(id,name,provider,base_url,status,scope,priority,weight,max_concurrency,max_user_concurrency,multiplier_card_supported,
			model_mapping,param_override,header_override,status_code_mapping,settings,tag,remark,auto_disable)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13::jsonb,$14::jsonb,$15::jsonb,$16::jsonb,NULLIF($17,''),$18,$19)
			ON CONFLICT(id) DO NOTHING`, c.ID, c.Name, provider, c.BaseURL, status, scope, c.Priority, c.Weight, c.Concurrency, c.UserConcurrency, c.CardSupported,
			jsonObject(c.ModelMapping), jsonObject(c.ParamOverride), jsonObject(c.HeaderOverride), jsonObject(c.StatusMapping), jsonObject(c.Settings), c.Tag, c.Remark, autoDisable)
		if err != nil {
			return fmt.Errorf("legacy: import channel %d: %w", c.ID, err)
		}
		if tag.RowsAffected() == 1 && c.ProxyURL != "" {
			if _, err = tx.Exec(ctx, `UPDATE v3_catalog.channels SET proxy_url=$2 WHERE id=$1`, c.ID, c.ProxyURL); err != nil {
				return err
			}
		}
		if tag.RowsAffected() == 0 {
			var targetProvider, targetName string
			if err = tx.QueryRow(ctx, `SELECT provider,name FROM v3_catalog.channels WHERE id=$1`, c.ID).Scan(&targetProvider, &targetName); err != nil {
				return err
			}
			if targetProvider != provider || targetName != c.Name {
				return fmt.Errorf("legacy: target channel ID %d differs from the source identity", c.ID)
			}
			continue
		} // preserve credentials on idempotent replay
		for index, secret := range splitSecrets(c.Key) {
			sealed, sealErr := m.crypto.Encrypt([]byte(secret))
			if sealErr != nil {
				return sealErr
			}
			kind, credentialStatus, expiresAt, propertiesErr := credentialProperties(secret, c.ChannelInfo, index)
			if propertiesErr != nil {
				return propertiesErr
			}
			if _, err = tx.Exec(ctx, `INSERT INTO v3_catalog.channel_credentials(channel_id,secret,kind,status,expires_at) VALUES($1,$2,$3,$4,$5)`, c.ID, sealed, kind, credentialStatus, expiresAt); err != nil {
				return err
			}
		}
		groups := list(c.Group)
		if len(groups) == 0 {
			groups = []string{"default"}
		}
		for _, group := range groups {
			if _, err = tx.Exec(ctx, `INSERT INTO v3_catalog.groups(name) VALUES($1) ON CONFLICT DO NOTHING`, group); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES($1,$2)`, c.ID, group); err != nil {
				return err
			}
		}
		for _, model := range list(c.Models) {
			if _, err = tx.Exec(ctx, `INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES($1,$2) ON CONFLICT DO NOTHING`, c.ID, model); err != nil {
				return err
			}
		}
	}
	return nil
}

// Credentials may contain commas (AWS, OAuth JSON), so only newlines split keys.
func splitSecrets(value string) []string {
	var out []string
	if json.Valid([]byte(strings.TrimSpace(value))) {
		return []string{strings.TrimSpace(value)}
	}
	for _, key := range strings.Split(value, "\n") {
		if key = strings.TrimSpace(key); key != "" {
			out = append(out, key)
		}
	}
	return out
}
