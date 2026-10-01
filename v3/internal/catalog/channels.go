package catalog

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func loadChannels(ctx context.Context, tx pgx.Tx, dec Decrypter) (map[int64]*Channel, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, name, provider, base_url, proxy_url, scope, coalesce(owner_user_id, 0),
		       priority, weight, max_concurrency, max_user_concurrency,
		       multiplier_card_supported, model_mapping, param_override,
		       header_override, settings, status_code_mapping
		FROM v3_catalog.channels
		WHERE status = 'enabled'
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("catalog: query channels: %w", err)
	}
	defer rows.Close()

	channels := make(map[int64]*Channel)
	var order []int64
	for rows.Next() {
		c := &Channel{}
		var modelMapping, paramOverride, headerOverride, settings, statusMapping []byte
		if err := rows.Scan(&c.ID, &c.Name, &c.Provider, &c.BaseURL, &c.ProxyURL, &c.Scope, &c.OwnerUserID,
			&c.Priority, &c.Weight, &c.MaxConcurrency, &c.MaxUserConcurrency,
			&c.MultiplierCardSupported, &modelMapping, &paramOverride, &headerOverride, &settings, &statusMapping); err != nil {
			return nil, fmt.Errorf("catalog: scan channel: %w", err)
		}
		if c.ModelMapping, err = unmarshalStringMap(modelMapping); err != nil {
			return nil, fmt.Errorf("catalog: channel %d model_mapping: %w", c.ID, err)
		}
		if c.ParamOverride, err = unmarshalAnyMap(paramOverride); err != nil {
			return nil, fmt.Errorf("catalog: channel %d param_override: %w", c.ID, err)
		}
		if c.HeaderOverride, err = unmarshalStringMap(headerOverride); err != nil {
			return nil, fmt.Errorf("catalog: channel %d header_override: %w", c.ID, err)
		}
		if c.Settings, err = unmarshalAnyMap(settings); err != nil {
			return nil, fmt.Errorf("catalog: channel %d settings: %w", c.ID, err)
		}
		c.MultiplierCardUserEnabled = CardUserEnabled(c.Settings, c.MultiplierCardSupported)
		if c.StatusCodeMapping, err = ParseStatusCodeMapping(statusMapping); err != nil {
			return nil, fmt.Errorf("catalog: channel %d status_code_mapping: %w", c.ID, err)
		}
		channels[c.ID] = c
		order = append(order, c.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	creds, err := loadCredentials(ctx, tx, dec)
	if err != nil {
		return nil, err
	}
	for _, id := range order {
		channels[id].Credentials = creds[id]
	}
	return channels, nil
}

func loadCredentials(ctx context.Context, tx pgx.Tx, dec Decrypter) (map[int64][]Credential, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, channel_id, kind, secret, expires_at, max_concurrency, fingerprint
		FROM v3_catalog.channel_credentials
		WHERE status = 'enabled'
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("catalog: query credentials: %w", err)
	}
	defer rows.Close()

	byChannel := make(map[int64][]Credential)
	for rows.Next() {
		var cr Credential
		var secret []byte
		var expires *time.Time // NULL = never expires, kept as the zero time
		if err := rows.Scan(&cr.ID, &cr.ChannelID, &cr.Kind, &secret, &expires, &cr.MaxConcurrency, &cr.Fingerprint); err != nil {
			return nil, fmt.Errorf("catalog: scan credential: %w", err)
		}
		if expires != nil {
			cr.ExpiresAt = *expires
		}
		plain, err := dec.Decrypt(secret)
		if err != nil {
			return nil, fmt.Errorf("catalog: decrypt credential %d: %w", cr.ID, err)
		}
		cr.Secret = string(plain)
		byChannel[cr.ChannelID] = append(byChannel[cr.ChannelID], cr)
	}
	return byChannel, rows.Err()
}
