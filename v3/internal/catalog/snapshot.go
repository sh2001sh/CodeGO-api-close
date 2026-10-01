// Package catalog compiles channels, credentials, groups, routes and prices
// into an immutable Snapshot that gateways hold in memory (plan §3).
//
// The types in this file are a cross-module contract: routing reads them,
// the compiler in this package produces them. Change them only together.
package catalog

import (
	"encoding/json"
	"time"
)

// Snapshot is one immutable, versioned view of the catalog. Readers never
// mutate it; a new version replaces it through an atomic pointer swap.
type Snapshot struct {
	Version  int64
	Built    time.Time
	Groups   map[string]Group
	Channels map[int64]*Channel // enabled channels only
	// Routes indexes candidates by group then model. The slices are shared and
	// must be treated as read-only.
	Routes map[string]map[string][]Route
	Prices map[string]Price // by model
	// Settings contains only non-sensitive runtime configuration.
	Settings             map[string]json.RawMessage
	AccountProfiles      map[int64]AccountProfile
	Market               MarketSnapshot
	SubscriptionPolicies map[string]SubscriptionPolicy
	Metadata             MetadataSnapshot
	OfficialPools        map[string]OfficialPool
}

// Group is a pricing and routing group.
type Group struct {
	Name       string
	Multiplier float64
}

// Channel is an enabled upstream channel with its usable credentials.
type Channel struct {
	ID                        int64
	Name                      string
	Provider                  string
	BaseURL                   string
	Scope                     string // "official" | "marketplace"
	OwnerUserID               int64  // 0 for official channels
	Priority                  int
	Weight                    int
	MaxConcurrency            int // 0 = unlimited
	MaxUserConcurrency        int
	MultiplierCardSupported   bool
	MultiplierCardUserEnabled bool
	ModelMapping              map[string]string // requested -> upstream
	ProxyURL                  string
	Credentials               []Credential // enabled only, stable order by ID
	Settings                  map[string]any
	ParamOverride             map[string]any
	HeaderOverride            map[string]string
	StatusCodeMapping         map[string]int
	Groups                    []string
}

// Credential is one schedulable secret of a channel.
type Credential struct {
	ID             int64
	MaxConcurrency int // 0 = unlimited
	ChannelID      int64
	Kind           string // "api_key" | "oauth"
	Secret         string // decrypted; never log
	ExpiresAt      time.Time
	Fingerprint    CredentialFingerprint
}

// CredentialFingerprint stays stable when an OAuth token is refreshed.
type CredentialFingerprint struct {
	UserAgent  string `json:"user_agent"`
	TLSProfile string `json:"tls_profile"`
}

// Route is one candidate channel for a (group, model) pair. Strategy comes
// from the route pool when one exists, otherwise "weighted" over channels
// that list the model and belong to the group.
type Route struct {
	ChannelID int64
	Priority  int // higher first
	Weight    int // >= 1
	Strategy  string
}

// Price is the absolute price of a model in micro-credits.
type Price struct {
	Model             string
	Mode              string // "per_token" | "per_request" | "expression"
	InputPerMTok      int64
	OutputPerMTok     int64
	CacheReadPerMTok  int64
	CacheWritePerMTok int64
	PerRequest        int64
	Rules             map[string]any
}

// UpstreamModel returns the model name to send upstream for a request.
func (c *Channel) UpstreamModel(requested string) string {
	if m, ok := c.ModelMapping[requested]; ok && m != "" {
		return m
	}
	return requested
}
