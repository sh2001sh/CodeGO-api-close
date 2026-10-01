package channelmarket

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

// Decimal values are parsed exactly before entering fixed point pricing.
func multiplier(number json.Number) (int64, error) {
	r, ok := new(big.Rat).SetString(string(number))
	if !ok || r.Sign() <= 0 || r.Cmp(big.NewRat(1000000, 1)) > 0 {
		return 0, ErrInvalid
	}
	r.Mul(r, big.NewRat(1000000, 1))
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(r.Num(), r.Denom(), rem)
	if new(big.Int).Lsh(rem, 1).Cmp(r.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() || q.Sign() <= 0 {
		return 0, ErrInvalid
	}
	return q.Int64(), nil
}

type CreateRequest struct {
	Provider                         string          `json:"provider_type"`
	Name                             string          `json:"name,omitempty"`
	SourceLabel                      string          `json:"source_label"`
	BaseURL                          string          `json:"base_url"`
	APIKey                           string          `json:"api_key"`
	Models                           []string        `json:"declared_models"`
	Prices                           json.RawMessage `json:"model_prices"`
	Multiplier                       json.Number     `json:"multiplier"`
	Visibility                       string          `json:"visibility"`
	MaxConcurrency                   int             `json:"max_concurrency"`
	UserMaxConcurrency               int             `json:"user_max_concurrency"`
	QPS                              json.Number     `json:"qps"`
	Maintenance                      string          `json:"maintenance_window"`
	SensitiveWordInterceptionEnabled *bool           `json:"sensitive_word_interception_enabled"`
	MultiplierCardSupported          bool            `json:"multiplier_card_supported"`
	MultiplierCardUserEnabled        bool            `json:"multiplier_card_user_enabled"`
	AutoProbeEnabled                 bool            `json:"auto_probe_enabled"`
	AutoProbeIntervalMinutes         int             `json:"auto_probe_interval_minutes"`
	AutoProbeModel                   string          `json:"auto_probe_model"`
}

func (r *CreateRequest) validate() (int64, error) {
	if err := r.validateScalarFields(); err != nil {
		return 0, err
	}
	factor, err := r.resolveMultiplier()
	if err != nil {
		return 0, err
	}
	if err := r.normalizeAndValidateModelsAndPrices(); err != nil {
		return 0, err
	}
	return factor, nil
}

// validateScalarFields validates and normalizes every scalar field except
// multiplier, models and prices (handled separately below).
func (r *CreateRequest) validateScalarFields() error {
	r.BaseURL = strings.TrimRight(strings.TrimSpace(r.BaseURL), "/")
	r.APIKey = strings.TrimSpace(r.APIKey)
	if !validUpstream(r.BaseURL) {
		return fmt.Errorf("%w: upstream URL", ErrInvalid)
	}
	switch r.Provider {
	case "openai_compatible", "openai", "codex", "azure_openai", "azure", "anthropic", "claude", "gemini":
	default:
		return ErrInvalid
	}
	if r.Provider == "" || len(r.Provider) > 64 || r.APIKey == "" || len(r.APIKey) > 131072 || len(r.Models) == 0 || len(r.Models) > 1000 || r.MaxConcurrency < 0 || r.UserMaxConcurrency < 0 || len(r.Name) > 255 || len(r.SourceLabel) > 40 {
		return ErrInvalid
	}
	if r.AutoProbeIntervalMinutes < 0 || r.AutoProbeIntervalMinutes > 1440 || len(r.AutoProbeModel) > 255 || len(r.Maintenance) > 64 {
		return ErrInvalid
	}
	if r.QPS != "" {
		q, ok := new(big.Rat).SetString(string(r.QPS))
		if !ok || q.Sign() < 0 || q.Cmp(big.NewRat(1000000, 1)) > 0 {
			return ErrInvalid
		}
	}
	if r.Visibility == "" {
		r.Visibility = "private"
	}
	if r.Visibility != "private" && r.Visibility != "public" && r.Visibility != "unlisted" {
		return ErrInvalid
	}
	return nil
}

// resolveMultiplier defaults and parses the multiplier field.
func (r *CreateRequest) resolveMultiplier() (int64, error) {
	if r.Multiplier == "" {
		r.Multiplier = "1"
	}
	return multiplier(r.Multiplier)
}

// normalizeAndValidateModelsAndPrices trims/dedupes the declared models list
// and validates the model-prices payload.
func (r *CreateRequest) normalizeAndValidateModelsAndPrices() error {
	seen := map[string]bool{}
	models := make([]string, 0, len(r.Models))
	for _, m := range r.Models {
		m = strings.TrimSpace(m)
		if m == "" || len(m) > 255 {
			return ErrInvalid
		}
		if !seen[m] {
			seen[m] = true
			models = append(models, m)
		}
	}
	r.Models = models
	if len(r.Prices) == 0 {
		r.Prices = json.RawMessage(`{}`)
	}
	var prices map[string]json.RawMessage
	if err := json.Unmarshal(r.Prices, &prices); err != nil || prices == nil {
		return ErrInvalid
	}
	if _, err := catalog.ParseMarketPrices(r.Prices); err != nil {
		return ErrInvalid
	}
	return nil
}

func nativeProvider(provider string) string {
	switch provider {
	case "openai_compatible":
		return "openai"
	case "azure_openai":
		return "azure"
	case "claude":
		return "anthropic"
	default:
		return provider
	}
}

func safeSettings(r CreateRequest) (json.RawMessage, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for _, key := range []string{"api_key", "base_url", "model_prices", "declared_models", "multiplier", "visibility", "name", "source_label", "provider_type"} {
		delete(fields, key)
	}
	return json.Marshal(fields)
}

// ChannelView is safe for owners and public users: no base URL or credential.
type ChannelPolicy struct {
	ModelConsistencyStatus           string      `json:"model_consistency_status"`
	SubmittedSourceLabel             string      `json:"submitted_source_label"`
	SourceLabelStatus                string      `json:"source_label_status"`
	SourceLabelReviewReason          string      `json:"source_label_review_reason"`
	MaxConcurrency                   int         `json:"max_concurrency"`
	UserMaxConcurrency               int         `json:"user_max_concurrency"`
	QPS                              json.Number `json:"qps"`
	Maintenance                      string      `json:"maintenance_window"`
	SensitiveWordInterceptionEnabled *bool       `json:"sensitive_word_interception_enabled"`
	MultiplierCardSupported          bool        `json:"multiplier_card_supported"`
	MultiplierCardUserEnabled        bool        `json:"multiplier_card_user_enabled"`
	AutoProbeEnabled                 bool        `json:"auto_probe_enabled"`
	AutoProbeIntervalMinutes         int         `json:"auto_probe_interval_minutes"`
	AutoProbeModel                   string      `json:"auto_probe_model"`
	AutoProbeLastAt                  *time.Time  `json:"auto_probe_last_at,omitempty"`
	AutoProbeLastStatus              string      `json:"auto_probe_last_status,omitempty"`
}
type ChannelView struct {
	ChannelPolicy
	VerificationView
	ID                string          `json:"id"`
	InternalChannelID int64           `json:"internal_channel_id"`
	OwnerUserID       int64           `json:"owner_user_id"`
	GroupID           string          `json:"group_id"`
	PublicSlug        string          `json:"public_slug"`
	Name              string          `json:"system_display_name"`
	Provider          string          `json:"provider_type"`
	SourceLabel       string          `json:"approved_source_label"`
	Models            []string        `json:"declared_models"`
	Prices            json.RawMessage `json:"model_prices"`
	MultiplierPPM     int64           `json:"multiplier_ppm"`
	Multiplier        json.Number     `json:"multiplier"`
	Visibility        string          `json:"visibility"`
	Status            string          `json:"lifecycle_status"`
	Verification      string          `json:"verification_status"`
	Reason            string          `json:"last_review_reason"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

const channelColumns = `g.public_channel_id,c.id,c.owner_user_id,g.id,g.public_slug,g.display_name,c.provider,g.source_label,
ARRAY(SELECT model FROM v3_catalog.channel_models WHERE channel_id=c.id ORDER BY model),g.model_prices,g.multiplier_ppm,
g.visibility,g.lifecycle_status,g.verification_status,g.review_reason,g.created_at,g.updated_at,coalesce(c.settings->'market','{}'),c.max_concurrency,c.max_user_concurrency,
coalesce((SELECT jsonb_build_object('id',v.id,'stage',v.stage,'detector_version',v.detector_version,'started_at',v.started_at,'completed_at',v.completed_at,'results',v.results) FROM v3_channelmarket.verification_runs v WHERE v.channel_id=c.id AND v.trigger<>'auto_probe' ORDER BY v.created_at DESC,v.id DESC LIMIT 1),'{}')`
const channelFrom = ` FROM v3_channelmarket.groups g JOIN v3_catalog.channels c ON c.id=g.channel_id `

type scanner interface{ Scan(...any) error }

func scanChannel(row scanner) (ChannelView, error) {
	var c ChannelView
	var policy, verification []byte
	var maximum, userMaximum int
	err := row.Scan(&c.ID, &c.InternalChannelID, &c.OwnerUserID, &c.GroupID, &c.PublicSlug, &c.Name, &c.Provider, &c.SourceLabel, &c.Models, &c.Prices, &c.MultiplierPPM, &c.Visibility, &c.Status, &c.Verification, &c.Reason, &c.CreatedAt, &c.UpdatedAt, &policy, &maximum, &userMaximum, &verification)
	if err == nil {
		err = json.Unmarshal(policy, &c.ChannelPolicy)
		c.MaxConcurrency = maximum
		c.UserMaxConcurrency = userMaximum
		if err == nil {
			c.VerificationView, err = parseVerificationView(verification)
		}
	}
	if c.Provider == "openai" {
		c.Provider = "openai_compatible"
	}
	if c.Provider == "azure" {
		c.Provider = "azure_openai"
	}
	if c.SubmittedSourceLabel == "" {
		c.SubmittedSourceLabel = c.SourceLabel
	}
	c.Multiplier = json.Number(fmt.Sprintf("%d.%06d", c.MultiplierPPM/1000000, c.MultiplierPPM%1000000))
	return c, err
}
