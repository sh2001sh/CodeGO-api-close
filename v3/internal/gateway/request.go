// Package gateway is the v3 relay pipeline:
//
//	Parse -> Authorize -> Plan -> Reserve -> Execute (attempt loop) -> Finalize
//
// All request state lives in Request and is passed explicitly between stages,
// replacing v2's 105 untyped gin context keys (plan §3).
package gateway

import (
	"net/netip"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/metrics"
)

// Protocol is the client-facing API shape of a request.
type Protocol uint8

const (
	ProtocolOpenAIChat Protocol = iota + 1
	ProtocolResponses
	ProtocolAnthropic
	ProtocolGemini
)

// Principal is the authenticated caller, resolved from an API key.
type Principal struct {
	UserID                      int64
	KeyID                       int64
	Group                       string
	MaxConcurrency              int
	RequestsPerMinute           int
	AllowedModels               []string
	AllowedCIDRs                []netip.Prefix
	BudgetLimited               bool
	BudgetAccountID             int64
	CrossGroupRetry             bool
	MaxMarketplaceMultiplierPPM int64
	AllowedGroups               []string
	AutoGroups                  []string
}

// Target is one routing candidate: a channel credential that can serve the model.
type Target struct {
	ChannelID                 int64
	CredentialID              int64
	Provider                  string // adapter id registered in Providers
	BaseURL                   string
	Secret                    string // decrypted credential, never logged
	UpstreamModel             string
	ProxyURL                  string
	MaxConcurrency            int
	CredentialMaxConcurrency  int
	MaxUserConcurrency        int
	Settings                  map[string]any
	ParamOverride             map[string]any
	HeaderOverride            map[string]string
	StatusCodeMapping         map[string]int
	Fingerprint               CredentialFingerprint
	Scope                     string
	OwnerUserID               int64
	Group                     string
	MultiplierPPM             int64
	RoutePoolID               int64
	ProcurementCostMultiplier string // frozen exact decimal, empty means unattributed
}

type CredentialFingerprint struct {
	UserAgent  string
	TLSProfile string
}

// Usage is token accounting for one request.
type Usage struct {
	PromptTokens        int64
	CompletionTokens    int64
	CachedTokens        int64
	CacheWriteTokens    int64
	CacheWrite1hTokens  int64
	ImageInputTokens    int64
	ImageOutputTokens   int64
	AudioInputTokens    int64
	AudioOutputTokens   int64
	ImageCount          int64 // actual generated images, independent of token usage
	AudioDurationMicros int64 // actual input audio duration in microseconds
	AudioCharacters     int64 // TTS input Unicode code points
	VideoDurationMicros int64 // actual generated video duration in microseconds
	ToolCalls           map[string]int64
	Estimated           bool // true when derived locally, not reported by the upstream
}

// Request carries everything the pipeline knows about one client request.
type Request struct {
	ID       string
	Received time.Time
	Timeline metrics.Timeline

	Protocol       Protocol
	Path           string            // original client endpoint for override conditions
	Body           []byte            // original client body; providers rewrite only what they must
	PricingHeaders map[string]string // frozen non-secret headers for expression rules
	Model          string
	Stream         bool
	ClientHeaders  map[string]string // explicit protocol allowlist, never auth headers

	Principal Principal
	Targets   []Target // ordered RoutePlan; retries walk it, never re-plan
	Reserve   any      // opaque reservation handle owned by the Settler

	Attempts []Attempt
	sample   *ResponseSample
}

// Attempt records one upstream try, for audit and scheduler feedback.
type Attempt struct {
	Target   Target
	Result   AttemptResult
	Duration time.Duration
}
