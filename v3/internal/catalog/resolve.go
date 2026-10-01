package catalog

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type ReferenceReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// ResolveReference is only for durable jobs accepted before a restart. Exact
// identifiers survive disabled channels, but deleted/replaced credentials
// fail rather than selecting a different account. New admission uses Compile.
func ResolveReference(ctx context.Context, q ReferenceReader, dec Decrypter, channelID, credentialID int64) (*Channel, Credential, error) {
	var channel Channel
	var credential Credential
	if channelID <= 0 || credentialID <= 0 || dec == nil {
		return nil, credential, fmt.Errorf("catalog: invalid stored target reference")
	}
	var modelMapping, params, headers, settings, statusMap, secret []byte
	var expiry *time.Time
	err := q.QueryRow(ctx, `SELECT c.id,c.name,c.provider,c.base_url,c.proxy_url,c.scope,coalesce(c.owner_user_id,0),c.max_concurrency,c.max_user_concurrency,c.multiplier_card_supported,c.model_mapping,c.param_override,c.header_override,c.settings,c.status_code_mapping,
 ARRAY(SELECT group_name FROM v3_catalog.channel_groups WHERE channel_id=c.id ORDER BY group_name),
 cr.id,cr.channel_id,cr.kind,cr.secret,cr.expires_at,cr.max_concurrency,cr.fingerprint
 FROM v3_catalog.channels c JOIN v3_catalog.channel_credentials cr ON cr.channel_id=c.id
 WHERE c.id=$1 AND cr.id=$2`, channelID, credentialID).Scan(&channel.ID, &channel.Name, &channel.Provider, &channel.BaseURL, &channel.ProxyURL, &channel.Scope, &channel.OwnerUserID, &channel.MaxConcurrency, &channel.MaxUserConcurrency, &channel.MultiplierCardSupported, &modelMapping, &params, &headers, &settings, &statusMap, &channel.Groups, &credential.ID, &credential.ChannelID, &credential.Kind, &secret, &expiry, &credential.MaxConcurrency, &credential.Fingerprint)
	if err != nil {
		return nil, credential, fmt.Errorf("catalog: resolve target reference: %w", err)
	}
	if channel.ModelMapping, err = unmarshalStringMap(modelMapping); err != nil {
		return nil, credential, err
	}
	if channel.ParamOverride, err = unmarshalAnyMap(params); err != nil {
		return nil, credential, err
	}
	if channel.HeaderOverride, err = unmarshalStringMap(headers); err != nil {
		return nil, credential, err
	}
	if channel.Settings, err = unmarshalAnyMap(settings); err != nil {
		return nil, credential, err
	}
	channel.MultiplierCardUserEnabled = CardUserEnabled(channel.Settings, channel.MultiplierCardSupported)
	if channel.StatusCodeMapping, err = ParseStatusCodeMapping(statusMap); err != nil {
		return nil, credential, err
	}
	plaintext, err := dec.Decrypt(secret)
	if err != nil {
		return nil, credential, fmt.Errorf("catalog: stored credential cannot be decrypted")
	}
	credential.Secret = string(plaintext)
	if expiry != nil {
		credential.ExpiresAt = *expiry
	}
	channel.Credentials = []Credential{credential}
	return &channel, credential, nil
}
