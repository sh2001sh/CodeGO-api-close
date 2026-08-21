package app

import "time"

import marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"

type ChannelModelPrice = marketplacedomain.ChannelModelPrice

const (
	ChannelBillingModeToken   = marketplacedomain.ChannelBillingModeToken
	ChannelBillingModePerCall = marketplacedomain.ChannelBillingModePerCall
)

type CreateChannelRequest struct {
	ProviderType                     string                       `json:"provider_type"`
	SourceLabel                      string                       `json:"source_label"`
	BaseURL                          string                       `json:"base_url"`
	APIKey                           string                       `json:"api_key"`
	DeclaredModels                   []string                     `json:"declared_models"`
	ModelPrices                      map[string]ChannelModelPrice `json:"model_prices"`
	Multiplier                       float64                      `json:"multiplier"`
	Visibility                       string                       `json:"visibility"`
	MaxConcurrency                   int                          `json:"max_concurrency"`
	UserMaxConcurrency               int                          `json:"user_max_concurrency"`
	QPS                              float64                      `json:"qps"`
	MaintenanceWindow                string                       `json:"maintenance_window"`
	SensitiveWordInterceptionEnabled *bool                        `json:"sensitive_word_interception_enabled"`
	AutoProbeEnabled                 bool                         `json:"auto_probe_enabled"`
	AutoProbeIntervalMinutes         int                          `json:"auto_probe_interval_minutes"`
	AutoProbeModel                   string                       `json:"auto_probe_model"`
}

type UpdateChannelRequest struct {
	ProviderType                     *string                       `json:"provider_type"`
	DeclaredModels                   *[]string                     `json:"declared_models"`
	ModelPrices                      *map[string]ChannelModelPrice `json:"model_prices"`
	Multiplier                       *float64                      `json:"multiplier"`
	Visibility                       *string                       `json:"visibility"`
	MaxConcurrency                   *int                          `json:"max_concurrency"`
	UserMaxConcurrency               *int                          `json:"user_max_concurrency"`
	QPS                              *float64                      `json:"qps"`
	MaintenanceWindow                *string                       `json:"maintenance_window"`
	SensitiveWordInterceptionEnabled *bool                         `json:"sensitive_word_interception_enabled"`
	AutoProbeEnabled                 *bool                         `json:"auto_probe_enabled"`
	AutoProbeIntervalMinutes         *int                          `json:"auto_probe_interval_minutes"`
	AutoProbeModel                   *string                       `json:"auto_probe_model"`
	BaseURL                          *string                       `json:"base_url"`
	APIKey                           *string                       `json:"api_key"`
	SourceLabel                      *string                       `json:"source_label"`
}

type AdminUpdateChannelRequest struct {
	UpdateChannelRequest
	ModelConsistencyStatus *string `json:"model_consistency_status"`
}

type ModelVerificationResult struct {
	Model     string    `json:"model"`
	Status    string    `json:"status"`
	Listed    bool      `json:"listed"`
	LatencyMS int64     `json:"latency_ms"`
	Error     string    `json:"error,omitempty"`
	TestedAt  time.Time `json:"tested_at"`
}

type GPT56MappingResult struct {
	RequestedModel string               `json:"requested_model"`
	ReportedModel  string               `json:"reported_model,omitempty"`
	Status         string               `json:"status"`
	LatencyMS      int64                `json:"latency_ms"`
	SampleCount    int                  `json:"sample_count"`
	MatchedSamples int                  `json:"matched_samples"`
	Samples        []GPT56MappingSample `json:"samples,omitempty"`
	Error          string               `json:"error,omitempty"`
	TestedAt       time.Time            `json:"tested_at"`
}

type GPT56MappingSample struct {
	Index         int       `json:"index"`
	Variant       string    `json:"variant,omitempty"`
	Status        string    `json:"status"`
	ReportedModel string    `json:"reported_model,omitempty"`
	LatencyMS     int64     `json:"latency_ms"`
	Error         string    `json:"error,omitempty"`
	TestedAt      time.Time `json:"tested_at"`
}

type GPT56MappingRunView struct {
	ID          string               `json:"id"`
	ParentRunID string               `json:"parent_run_id,omitempty"`
	Level       string               `json:"level"`
	Trigger     string               `json:"trigger"`
	Status      string               `json:"status"`
	Results     []GPT56MappingResult `json:"results"`
	StartedAt   time.Time            `json:"started_at"`
	CompletedAt *time.Time           `json:"completed_at"`
}

type ChannelView struct {
	ID                               string                       `json:"id"`
	OwnerUserID                      int                          `json:"owner_user_id"`
	OwnerExternalID                  string                       `json:"owner_external_id"`
	GroupID                          string                       `json:"group_id"`
	PublicSlug                       string                       `json:"public_slug"`
	SystemDisplayName                string                       `json:"system_display_name"`
	ProviderType                     string                       `json:"provider_type"`
	SubmittedSourceLabel             string                       `json:"submitted_source_label"`
	ApprovedSourceLabel              string                       `json:"approved_source_label"`
	SourceLabelStatus                string                       `json:"source_label_status"`
	SourceLabelReviewReason          string                       `json:"source_label_review_reason"`
	CredentialTail                   string                       `json:"credential_tail"`
	CredentialVersion                int                          `json:"credential_version"`
	DeclaredModels                   []string                     `json:"declared_models"`
	ModelPrices                      map[string]ChannelModelPrice `json:"model_prices"`
	ModelVerificationResults         []ModelVerificationResult    `json:"model_verification_results"`
	ConnectivityTestStatus           string                       `json:"connectivity_test_status"`
	ConnectivityTestCheckedAt        *time.Time                   `json:"connectivity_test_checked_at"`
	ModelConsistencyStatus           string                       `json:"model_consistency_status"`
	GPT56MappingResults              []GPT56MappingResult         `json:"gpt56_mapping_results"`
	GPT56MappingStatus               string                       `json:"gpt56_mapping_status"`
	GPT56MappingCheckedAt            *time.Time                   `json:"gpt56_mapping_checked_at"`
	GPT56MappingLevel                string                       `json:"gpt56_mapping_level"`
	GPT56MappingTrigger              string                       `json:"gpt56_mapping_trigger"`
	GPT56MappingHistory              []GPT56MappingRunView        `json:"gpt56_mapping_history"`
	AutoProbeEnabled                 bool                         `json:"auto_probe_enabled"`
	AutoProbeIntervalMinutes         int                          `json:"auto_probe_interval_minutes"`
	AutoProbeModel                   string                       `json:"auto_probe_model"`
	AutoProbeLastStatus              string                       `json:"auto_probe_last_status"`
	AutoProbeLastAt                  *time.Time                   `json:"auto_probe_last_at"`
	Multiplier                       float64                      `json:"multiplier"`
	LifecycleStatus                  string                       `json:"lifecycle_status"`
	VerificationStatus               string                       `json:"verification_status"`
	VerificationStage                string                       `json:"verification_stage"`
	VerificationSummary              string                       `json:"verification_summary"`
	VerificationDetectorVersion      string                       `json:"verification_detector_version"`
	VerificationStartedAt            *time.Time                   `json:"verification_started_at"`
	VerificationCompletedAt          *time.Time                   `json:"verification_completed_at"`
	Visibility                       string                       `json:"visibility"`
	MaxConcurrency                   int                          `json:"max_concurrency"`
	UserMaxConcurrency               int                          `json:"user_max_concurrency"`
	QPS                              float64                      `json:"qps"`
	MaintenanceWindow                string                       `json:"maintenance_window"`
	SensitiveWordInterceptionEnabled bool                         `json:"sensitive_word_interception_enabled"`
	InternalChannelID                *int                         `json:"internal_channel_id"`
	LastReviewReason                 string                       `json:"last_review_reason"`
	VerificationDueAt                *time.Time                   `json:"verification_due_at"`
	RequestCount                     int64                        `json:"request_count"`
	TotalIncome                      int64                        `json:"total_income"`
	PendingIncome                    int64                        `json:"pending_income"`
	ReleasedIncome                   int64                        `json:"released_income"`
	CreatedAt                        time.Time                    `json:"created_at"`
	UpdatedAt                        time.Time                    `json:"updated_at"`
}

type AdminChannelQuery struct {
	Search         string
	Status         string
	Source         string
	Provider       string
	Verification   string
	MappingStatus  string
	OwnerSearch    string
	StartTimestamp int64
	EndTimestamp   int64
}

type AdminOwnerIncomeQuery struct {
	OwnerSearch    string
	StartTimestamp int64
	EndTimestamp   int64
}

type AdminOwnerIncomeItem struct {
	OwnerUserID     int    `json:"owner_user_id"`
	OwnerExternalID string `json:"owner_external_id"`
	RequestCount    int64  `json:"request_count"`
	TotalIncome     int64  `json:"total_income"`
	PendingIncome   int64  `json:"pending_income"`
	ReleasedIncome  int64  `json:"released_income"`
}

type AdminOwnerIncomeResult struct {
	Items          []AdminOwnerIncomeItem `json:"items"`
	OwnerCount     int                    `json:"owner_count"`
	RequestCount   int64                  `json:"request_count"`
	TotalIncome    int64                  `json:"total_income"`
	PendingIncome  int64                  `json:"pending_income"`
	ReleasedIncome int64                  `json:"released_income"`
}

type GroupQuery struct {
	ViewerUserID  int
	Search        string
	Model         string
	Source        string
	Provider      string
	Status        string
	Verification  string
	Sort          string
	Direction     string
	WindowHours   int
	Page          int
	PageSize      int
	MinMultiplier float64
	MaxMultiplier float64
}

type GroupListItem struct {
	ID                         string                    `json:"id"`
	ChannelID                  string                    `json:"channel_id"`
	PublicSlug                 string                    `json:"public_slug"`
	SystemDisplayName          string                    `json:"system_display_name"`
	SourceType                 string                    `json:"source_type"`
	SourceLabel                string                    `json:"source_label"`
	ProviderType               string                    `json:"provider_type"`
	CreditPoolPolicy           string                    `json:"credit_pool_policy"`
	LifecycleStatus            string                    `json:"lifecycle_status"`
	VerificationStatus         string                    `json:"verification_status"`
	VerificationDueAt          *time.Time                `json:"verification_due_at"`
	VerificationCompletedAt    *time.Time                `json:"verification_completed_at"`
	Multiplier                 float64                   `json:"multiplier"`
	SubscriptionEnabled        bool                      `json:"subscription_enabled"`
	SubscriptionMultiplier     float64                   `json:"subscription_multiplier"`
	Models                     []string                  `json:"models"`
	ModelVerificationResults   []ModelVerificationResult `json:"model_verification_results"`
	ModelConsistencyStatus     string                    `json:"model_consistency_status"`
	GPT56MappingResults        []GPT56MappingResult      `json:"gpt56_mapping_results"`
	GPT56MappingStatus         string                    `json:"gpt56_mapping_status"`
	GPT56MappingCheckedAt      *time.Time                `json:"gpt56_mapping_checked_at"`
	GPT56MappingLevel          string                    `json:"gpt56_mapping_level"`
	GPT56MappingTrigger        string                    `json:"gpt56_mapping_trigger"`
	ConnectivityTestStatus     string                    `json:"connectivity_test_status"`
	ConnectivityTestCheckedAt  *time.Time                `json:"connectivity_test_checked_at"`
	ChannelFeedback            ChannelFeedbackSummary    `json:"channel_feedback"`
	CanSubmitChannelFeedback   bool                      `json:"can_submit_channel_feedback"`
	ChannelFeedbackPermission  string                    `json:"channel_feedback_permission"`
	Rank                       int                       `json:"rank"`
	Score                      float64                   `json:"score"`
	SuccessRate                float64                   `json:"success_rate"`
	WilsonSuccessRate          float64                   `json:"wilson_success_rate"`
	AvgTTFTMs                  float64                   `json:"avg_ttft_ms"`
	AttemptTTFTP50Ms           float64                   `json:"attempt_ttft_p50_ms"`
	AttemptTTFTP95Ms           float64                   `json:"attempt_ttft_p95_ms"`
	E2ETTFTP50Ms               float64                   `json:"e2e_ttft_p50_ms"`
	E2ETTFTP95Ms               float64                   `json:"e2e_ttft_p95_ms"`
	LatencySampleCount         int64                     `json:"latency_sample_count"`
	AvgLatencyMs               float64                   `json:"avg_latency_ms"`
	AvgTPS                     float64                   `json:"avg_tps"`
	CacheHitRate               float64                   `json:"cache_hit_rate"`
	LatestRequestStatus        string                    `json:"latest_request_status"`
	RecentRequestSeries        []RecentRequestBucket     `json:"recent_request_series"`
	RecentRequestBucketSeconds int64                     `json:"recent_request_bucket_seconds"`
	RequestCount               int64                     `json:"request_count"`
	MaxConcurrency             int                       `json:"max_concurrency"`
	UserMaxConcurrency         int                       `json:"user_max_concurrency"`
	CurrentConcurrency         int                       `json:"current_concurrency"`
	IndependentConsumers       int64                     `json:"-"`
	Observing                  bool                      `json:"observing"`
	UpdatedAt                  time.Time                 `json:"updated_at"`
}

type ChannelFeedbackRequest struct {
	Status string `json:"status"`
}

type ChannelFeedbackSummary struct {
	Passed       int64  `json:"passed"`
	Failed       int64  `json:"failed"`
	Questionable int64  `json:"questionable"`
	Total        int64  `json:"total"`
	ViewerStatus string `json:"viewer_status"`
}

type RecentRequestBucket struct {
	Ts           int64   `json:"ts"`
	SuccessRate  float64 `json:"success_rate"`
	RequestCount int64   `json:"request_count"`
}

type GroupHighlight struct {
	GroupID           string  `json:"group_id"`
	SystemDisplayName string  `json:"system_display_name"`
	Score             float64 `json:"score"`
	Multiplier        float64 `json:"multiplier"`
	AvgTTFTMs         float64 `json:"avg_ttft_ms"`
	AttemptTTFTP50Ms  float64 `json:"attempt_ttft_p50_ms"`
}

type GroupHighlights struct {
	Best     *GroupHighlight `json:"best"`
	Cheapest *GroupHighlight `json:"cheapest"`
	Fastest  *GroupHighlight `json:"fastest"`
}

type GroupListResult struct {
	Items       []GroupListItem `json:"items"`
	Highlights  GroupHighlights `json:"highlights"`
	Total       int             `json:"total"`
	Page        int             `json:"page"`
	PageSize    int             `json:"page_size"`
	RankedCount int             `json:"ranked_count"`
	WindowHours int             `json:"window_hours"`
}

type MultiplierTrendQuery struct {
	RangeHours int
	Model      string
}

type MultiplierTrendPoint struct {
	Timestamp       int64    `json:"timestamp"`
	ReliableMin     *float64 `json:"reliable_min"`
	ListedMin       *float64 `json:"listed_min"`
	Median          *float64 `json:"median"`
	ReliableGroupID string   `json:"reliable_group_id,omitempty"`
	EligibleCount   int      `json:"eligible_count"`
	TotalCount      int      `json:"total_count"`
}

type MultiplierTrendSource struct {
	Source string                 `json:"source"`
	Points []MultiplierTrendPoint `json:"points"`
}

type MultiplierTrendResult struct {
	RangeHours    int                     `json:"range_hours"`
	BucketSeconds int64                   `json:"bucket_seconds"`
	Models        []string                `json:"models"`
	Sources       []MultiplierTrendSource `json:"sources"`
}

type TokenBindingRequest struct {
	TokenID int `json:"token_id"`
}

type RemoveFailedModelRequest struct {
	Model string `json:"model"`
}

type FetchModelsRequest struct {
	ProviderType string `json:"provider_type"`
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
}

type AdminReviewRequest struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason"`
}

type RoutingBinding struct {
	RouteKey         string
	GroupID          string
	InternalGroup    string
	OwnerUserID      int
	SourceType       string
	CreditPoolPolicy string
	Multiplier       float64
	ModelPrices      map[string]ChannelModelPrice
	Models           []string
}

type AutoRoutePoolUpdateRequest struct {
	GroupIDs []string `json:"group_ids"`
}

type AutoRoutePoolItem struct {
	GroupID             string   `json:"group_id"`
	SourceType          string   `json:"source_type"`
	PublicSlug          string   `json:"public_slug"`
	SystemDisplayName   string   `json:"system_display_name"`
	SourceLabel         string   `json:"source_label"`
	LifecycleStatus     string   `json:"lifecycle_status"`
	Multiplier          float64  `json:"multiplier"`
	Availability        float64  `json:"availability"`
	SuccessRate         float64  `json:"success_rate"`
	CacheHitRate        float64  `json:"cache_hit_rate"`
	AvgLatencyMS        float64  `json:"avg_latency_ms"`
	LatestRequestStatus string   `json:"latest_request_status"`
	MetricsAvailable    bool     `json:"metrics_available"`
	RouteScore          float64  `json:"route_score"`
	Observing           bool     `json:"observing"`
	RequestCount        int64    `json:"request_count"`
	Models              []string `json:"models"`
	Selected            bool     `json:"selected"`
	Priority            int      `json:"priority"`
}

type AutoRoutePoolView struct {
	TokenGroup    string              `json:"token_group"`
	SelectedCount int                 `json:"selected_count"`
	Items         []AutoRoutePoolItem `json:"items"`
}
