package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Dynamic pricing rules can contain int64 amounts greater than 2^53.
// Preserve those numbers through Redis instead of converting them to float64.
func decodeWireSnapshot(blob []byte) (*wireSnapshot, error) {
	decoder := json.NewDecoder(bytes.NewReader(blob))
	decoder.UseNumber()
	var w wireSnapshot
	if err := decoder.Decode(&w); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("catalog: snapshot contains trailing data")
	}
	return &w, nil
}

// The wire format mirrors Snapshot (snapshot.go) field for field, except
// Credential.Secret: on the wire it is AES-GCM ciphertext, never plaintext.
// Redis is a shared cache with a longer, fuzzier trust boundary than the
// database connection to PostgreSQL, so a snapshot blob sitting in Redis
// carries the same protection as a row in channel_credentials. The gateway's
// Store decrypts each credential once, right after a snapshot loads, with
// the same Decrypter Compile uses; nothing downstream of Store ever sees
// ciphertext.
type wireSnapshot struct {
	Version              int64                         `json:"version"`
	Built                time.Time                     `json:"built"`
	Groups               map[string]Group              `json:"groups"`
	Channels             map[int64]*wireChannel        `json:"channels"`
	Routes               map[string]map[string][]Route `json:"routes"`
	Prices               map[string]Price              `json:"prices"`
	Settings             map[string]json.RawMessage    `json:"settings"`
	AccountProfiles      map[int64]AccountProfile      `json:"account_profiles"`
	Market               MarketSnapshot                `json:"market"`
	SubscriptionPolicies map[string]SubscriptionPolicy `json:"subscription_policies"`
	Metadata             MetadataSnapshot              `json:"metadata"`
	OfficialPools        map[string]OfficialPool       `json:"official_pools"`
}

type wireChannel struct {
	ID                        int64             `json:"id"`
	Name                      string            `json:"name"`
	Provider                  string            `json:"provider"`
	BaseURL                   string            `json:"base_url"`
	Scope                     string            `json:"scope"`
	OwnerUserID               int64             `json:"owner_user_id"`
	Priority                  int               `json:"priority"`
	Weight                    int               `json:"weight"`
	MaxConcurrency            int               `json:"max_concurrency"`
	MaxUserConcurrency        int               `json:"max_user_concurrency"`
	MultiplierCardSupported   bool              `json:"multiplier_card_supported"`
	MultiplierCardUserEnabled bool              `json:"multiplier_card_user_enabled"`
	ModelMapping              map[string]string `json:"model_mapping,omitempty"`
	ProxyURL                  string            `json:"proxy_url"`
	Credentials               []wireCredential  `json:"credentials"`
	Settings                  map[string]any    `json:"settings,omitempty"`
	ParamOverride             map[string]any    `json:"param_override,omitempty"`
	HeaderOverride            map[string]string `json:"header_override,omitempty"`
	StatusCodeMapping         map[string]int    `json:"status_code_mapping,omitempty"`
	Groups                    []string          `json:"groups,omitempty"`
}

type wireCredential struct {
	ID             int64                 `json:"id"`
	ChannelID      int64                 `json:"channel_id"`
	Kind           string                `json:"kind"`
	Ciphertext     []byte                `json:"ciphertext"` // AES-GCM, nonce-prefixed
	ExpiresAt      time.Time             `json:"expires_at"`
	MaxConcurrency int                   `json:"max_concurrency"`
	Fingerprint    CredentialFingerprint `json:"fingerprint"`
}

// sealSnapshot re-encrypts every credential's plaintext secret so the
// resulting blob is safe to store in Redis.
func sealSnapshot(snap *Snapshot, enc Encrypter) (*wireSnapshot, error) {
	w := &wireSnapshot{
		Version:              snap.Version,
		Built:                snap.Built,
		Groups:               snap.Groups,
		Channels:             make(map[int64]*wireChannel, len(snap.Channels)),
		Routes:               snap.Routes,
		Prices:               snap.Prices,
		Settings:             snap.Settings,
		AccountProfiles:      snap.AccountProfiles,
		Market:               snap.Market,
		SubscriptionPolicies: snap.SubscriptionPolicies,
		Metadata:             snap.Metadata,
		OfficialPools:        snap.OfficialPools,
	}
	for id, c := range snap.Channels {
		wc := &wireChannel{
			ID: c.ID, Name: c.Name, Provider: c.Provider, BaseURL: c.BaseURL,
			Scope: c.Scope, OwnerUserID: c.OwnerUserID, Priority: c.Priority,
			Weight: c.Weight, MaxConcurrency: c.MaxConcurrency,
			MaxUserConcurrency:        c.MaxUserConcurrency,
			MultiplierCardSupported:   c.MultiplierCardSupported,
			MultiplierCardUserEnabled: c.MultiplierCardUserEnabled,
			ModelMapping:              c.ModelMapping, ProxyURL: c.ProxyURL,
			Settings: c.Settings, ParamOverride: c.ParamOverride,
			HeaderOverride:    c.HeaderOverride,
			StatusCodeMapping: c.StatusCodeMapping,
			Groups:            c.Groups,
		}
		wc.Credentials = make([]wireCredential, len(c.Credentials))
		for i, cr := range c.Credentials {
			ct, err := enc.Encrypt([]byte(cr.Secret))
			if err != nil {
				return nil, fmt.Errorf("catalog: encrypt credential %d: %w", cr.ID, err)
			}
			wc.Credentials[i] = wireCredential{
				ID: cr.ID, ChannelID: cr.ChannelID, Kind: cr.Kind,
				Ciphertext: ct, ExpiresAt: cr.ExpiresAt, MaxConcurrency: cr.MaxConcurrency, Fingerprint: cr.Fingerprint,
			}
		}
		w.Channels[id] = wc
	}
	return w, nil
}

// openSnapshot reverses sealSnapshot, decrypting every credential's secret
// back to plaintext for in-process use.
func openSnapshot(w *wireSnapshot, dec Decrypter) (*Snapshot, error) {
	snap := &Snapshot{
		Version:              w.Version,
		Built:                w.Built,
		Groups:               w.Groups,
		Channels:             make(map[int64]*Channel, len(w.Channels)),
		Routes:               w.Routes,
		Prices:               w.Prices,
		Settings:             w.Settings,
		AccountProfiles:      w.AccountProfiles,
		Market:               w.Market,
		SubscriptionPolicies: w.SubscriptionPolicies,
		Metadata:             w.Metadata,
		OfficialPools:        w.OfficialPools,
	}
	for id, wc := range w.Channels {
		c := &Channel{
			ID: wc.ID, Name: wc.Name, Provider: wc.Provider, BaseURL: wc.BaseURL,
			Scope: wc.Scope, OwnerUserID: wc.OwnerUserID, Priority: wc.Priority,
			Weight: wc.Weight, MaxConcurrency: wc.MaxConcurrency,
			MaxUserConcurrency:        wc.MaxUserConcurrency,
			MultiplierCardSupported:   wc.MultiplierCardSupported,
			MultiplierCardUserEnabled: CardUserEnabled(wc.Settings, wc.MultiplierCardUserEnabled || wc.MultiplierCardSupported),
			ModelMapping:              wc.ModelMapping, ProxyURL: wc.ProxyURL,
			Settings: wc.Settings, ParamOverride: wc.ParamOverride,
			HeaderOverride:    wc.HeaderOverride,
			StatusCodeMapping: wc.StatusCodeMapping,
			Groups:            wc.Groups,
		}
		c.Credentials = make([]Credential, len(wc.Credentials))
		for i, wcr := range wc.Credentials {
			plain, err := dec.Decrypt(wcr.Ciphertext)
			if err != nil {
				return nil, fmt.Errorf("catalog: decrypt credential %d: %w", wcr.ID, err)
			}
			c.Credentials[i] = Credential{
				ID: wcr.ID, ChannelID: wcr.ChannelID, Kind: wcr.Kind,
				Secret: string(plain), ExpiresAt: wcr.ExpiresAt, MaxConcurrency: wcr.MaxConcurrency, Fingerprint: wcr.Fingerprint,
			}
		}
		snap.Channels[id] = c
	}
	return snap, nil
}
