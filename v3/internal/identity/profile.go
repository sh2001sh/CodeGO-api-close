// Package identity authorizes API keys for the gateway without touching
// PostgreSQL on the hot path (plan §3; cache hardening borrowed from sub2api):
//
//	L1 in-process LRU -> L2 Redis -> PostgreSQL (singleflight + semaphore)
//
// Unknown keys are cached only in L1, so key scans never write to Redis.
// Invalidations arrive over redisx.ChannelInvalidate from the outbox relay.
package identity

import (
	"crypto/sha256"
	"errors"
	"net/netip"
	"slices"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// KeyProfile is everything authorization needs about one API key. It is
// immutable once cached.
type KeyProfile struct {
	KeyID                       int64          `json:"key_id"`
	MaxConcurrency              int            `json:"max_concurrency"`
	RequestsPerMinute           int            `json:"requests_per_minute"`
	UserID                      int64          `json:"user_id"`
	Group                       string         `json:"group"`          // key group, else the user's group
	Revoked                     bool           `json:"revoked"`        // key or user disabled/deleted
	ExpiresAt                   time.Time      `json:"expires_at"`     // zero = never
	AllowedModels               []string       `json:"allowed_models"` // nil = all; empty denies all
	AllowedCIDRs                []netip.Prefix `json:"allowed_cidrs"`  // nil = any; empty denies all
	BudgetLimited               bool           `json:"budget_limited"`
	CrossGroupRetry             bool           `json:"cross_group_retry"`
	MaxMarketplaceMultiplierPPM int64          `json:"max_marketplace_multiplier_ppm"`
	BudgetAccountID             int64          `json:"budget_account_id"`
	AllowedGroups               []string       `json:"allowed_groups"`
	AutoGroups                  []string       `json:"auto_groups"`
}

// Principal is the gateway's view of the key.
func (p *KeyProfile) Principal() gateway.Principal {
	return gateway.Principal{UserID: p.UserID, KeyID: p.KeyID, Group: p.Group,
		MaxConcurrency: p.MaxConcurrency, RequestsPerMinute: p.RequestsPerMinute,
		AllowedModels: p.AllowedModels, AllowedCIDRs: p.AllowedCIDRs, BudgetLimited: p.BudgetLimited, BudgetAccountID: p.BudgetAccountID,
		CrossGroupRetry: p.CrossGroupRetry, MaxMarketplaceMultiplierPPM: p.MaxMarketplaceMultiplierPPM, AllowedGroups: p.AllowedGroups, AutoGroups: p.AutoGroups}
}

// usable checks the parts of a profile that change with time. Expiry is
// checked on every read, not only at load, so a cached key stops working the
// moment it expires.
func (p *KeyProfile) usable(now time.Time) error {
	if p.Revoked {
		return gateway.ErrInvalidKey
	}
	if !p.ExpiresAt.IsZero() && !now.Before(p.ExpiresAt) {
		return gateway.ErrInvalidKey
	}
	return nil
}

// ErrModelNotAllowed and ErrAddressNotAllowed report per-key restrictions.
var (
	ErrModelNotAllowed   = errors.New("identity: model not allowed for this key")
	ErrAddressNotAllowed = errors.New("identity: client address not allowed for this key")
)

// AllowsModel reports whether the key may call model.
func (p *KeyProfile) AllowsModel(model string) bool {
	return p.AllowedModels == nil || slices.Contains(p.AllowedModels, model)
}

// AllowsAddr reports whether the key may be used from addr.
func (p *KeyProfile) AllowsAddr(addr netip.Addr) bool {
	if p.AllowedCIDRs == nil {
		return true
	}
	addr = addr.Unmap()
	for _, prefix := range p.AllowedCIDRs {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// HashKey is the lookup key stored in api_keys.key_hash.
func HashKey(apiKey string) [32]byte {
	return sha256.Sum256([]byte(apiKey))
}
