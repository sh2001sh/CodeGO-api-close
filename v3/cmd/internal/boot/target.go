package boot

import (
	"context"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// ResolveTarget restores an already accepted task's exact credential reference.
// It is used by durable task paths, outside normal gateway admission and routing.
func (d *Deps) ResolveTarget(ctx context.Context, channelID, credentialID int64) (gateway.Target, error) {
	channel, credential, err := catalog.ResolveReference(ctx, d.PG.Pool, d.Crypto, channelID, credentialID)
	if err != nil {
		return gateway.Target{}, err
	}
	return gateway.Target{
		ChannelID: channel.ID, CredentialID: credential.ID, Provider: channel.Provider,
		BaseURL: channel.BaseURL, Secret: credential.Secret, ProxyURL: channel.ProxyURL,
		MaxConcurrency: channel.MaxConcurrency, CredentialMaxConcurrency: credential.MaxConcurrency,
		MaxUserConcurrency: channel.MaxUserConcurrency, Settings: channel.Settings,
		ParamOverride: channel.ParamOverride, HeaderOverride: channel.HeaderOverride,
		StatusCodeMapping: channel.StatusCodeMapping, Scope: channel.Scope, OwnerUserID: channel.OwnerUserID,
		Fingerprint: gateway.CredentialFingerprint{UserAgent: credential.Fingerprint.UserAgent, TLSProfile: credential.Fingerprint.TLSProfile},
	}, nil
}
