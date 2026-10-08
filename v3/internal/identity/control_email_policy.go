package identity

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

func (c *Control) accountEmailSettings(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := c.pool.Query(ctx, `SELECT key,value,sensitive FROM v3_platform.settings WHERE key=ANY($1::text[])`,
		[]string{"EmailVerificationEnabled", "EmailDomainRestrictionEnabled", "EmailDomainWhitelist", "EmailAliasRestrictionEnabled"})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var key string
		var value []byte
		var sensitive bool
		if err = rows.Scan(&key, &value, &sensitive); err != nil {
			return nil, err
		}
		if sensitive {
			return nil, ErrInvalidInput
		}
		out[key] = value
	}
	return out, rows.Err()
}

func emailSettingBool(raw json.RawMessage) (bool, error) {
	if len(raw) == 0 {
		return false, nil
	}
	text, err := decodeBuiltinOAuthValue(raw)
	if err != nil {
		return false, ErrInvalidInput
	}
	value, err := strconv.ParseBool(text)
	if err != nil {
		return false, ErrInvalidInput
	}
	return value, nil
}

func (c *Control) registrationEmailRequired(ctx context.Context) (bool, error) {
	if c.cfg.RequireEmailVerification {
		return true, nil
	}
	settings, err := c.accountEmailSettings(ctx)
	if err != nil {
		return false, err
	}
	return emailSettingBool(settings["EmailVerificationEnabled"])
}

func (c *Control) validateEmailPolicy(ctx context.Context, email string, registration bool) error {
	settings, err := c.accountEmailSettings(ctx)
	if err != nil {
		return err
	}
	aliasRestricted, err := emailSettingBool(settings["EmailAliasRestrictionEnabled"])
	if err != nil {
		return err
	}
	domainRestricted, err := emailSettingBool(settings["EmailDomainRestrictionEnabled"])
	if err != nil {
		return err
	}
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return ErrInvalidInput
	}
	if (registration || aliasRestricted) && strings.ContainsAny(parts[0], "+.") {
		return ErrInvalidInput
	}
	if !domainRestricted {
		return nil
	}
	var domains []string
	if json.Unmarshal(settings["EmailDomainWhitelist"], &domains) != nil {
		var text string
		if json.Unmarshal(settings["EmailDomainWhitelist"], &text) != nil || text == "" {
			return ErrInvalidInput
		}
		// v2 option updates persist comma-separated domain strings.
		domains = strings.Split(text, ",")
	}
	for _, domain := range domains {
		if strings.EqualFold(strings.TrimSpace(domain), parts[1]) {
			return nil
		}
	}
	return ErrForbidden
}
