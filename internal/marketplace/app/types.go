package app

import "time"

import marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"

type ChannelModelPrice = marketplacedomain.ChannelModelPrice

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
	Status        string    `json:"status"`
	ReportedModel string    `json:"reported_model,omitempty"`
	LatencyMS     int64     `json:"latency_ms"`
	Error         string    `json:"error,omitempty"`
	TestedAt      time.Time `json:"tested_at"`
}

type ChannelView struct {
	ID                               string                       `json:"id"`
	OwnerUserID                      int                          `json:"owner_user_id"`
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
	Status         string
	StartTimestamp int64
	EndTimestamp   int64
}

type AdminOwnerIncomeQuery struct {
	StartTimestamp int64
	EndTimestamp   int64
}

type AdminOwnerIncomeItem struct {
	OwnerUserID    int   `json:"owner_user_id"`
	RequestCount   int64 `json:"request_count"`
	TotalIncome    int64 `json:"total_income"`
	PendingIncome  int64 `json:"pending_income"`
	ReleasedIncome int64 `json:"released_income"`
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
	Models                     []string                  `json:"models"`
	ModelVerificationResults   []ModelVerificationResult `json:"model_verification_results"`
	ModelConsistencyStatus     string                    `json:"model_consistency_status"`
	GPT56MappingResults        []GPT56MappingResult      `json:"gpt56_mapping_results"`
	GPT56MappingStatus         string                    `json:"gpt56_mapping_status"`
	GPT56MappingCheckedAt      *time.Time                `json:"gpt56_mapping_checked_at"`
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
	AvgLatencyMs               float64                   `json:"avg_latency_ms"`
	AvgTPS                     float64                   `json:"avg_tps"`
	CacheHitRate               float64                   `json:"cache_hit_rate"`
	LatestRequestStatus        string                    `json:"latest_request_status"`
	RecentRequestSeries        []RecentRequestBucket     `json:"recent_request_series"`
	RecentRequestBucketSeconds int64                     `json:"recent_request_bucket_seconds"`
	RequestCount               int64                     `json:"request_count"`
	MaxConcurrency             int                       `json:"max_concurrency"`
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

type TokenBindingRequest struct {
	TokenID int `json:"token_id"`
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
	GroupID           string   `json:"group_id"`
	SourceType        string   `json:"source_type"`
	PublicSlug        string   `json:"public_slug"`
	SystemDisplayName string   `json:"system_display_name"`
	SourceLabel       string   `json:"source_label"`
	LifecycleStatus   string   `json:"lifecycle_status"`
	Multiplier        float64  `json:"multiplier"`
	Availability      float64  `json:"availability"`
	RouteScore        float64  `json:"route_score"`
	Observing         bool     `json:"observing"`
	RequestCount      int64    `json:"request_count"`
	Models            []string `json:"models"`
	Selected          bool     `json:"selected"`
	Priority          int      `json:"priority"`
}

type AutoRoutePoolView struct {
	TokenGroup    string              `json:"token_group"`
	SelectedCount int                 `json:"selected_count"`
	Items         []AutoRoutePoolItem `json:"items"`
}

type OwnerUsageLogQuery struct {
	ChannelID      string
	StartTimestamp int64
	EndTimestamp   int64
	Page           int
	PageSize       int
}

type OwnerUsageLogItem struct {
	ID                 int        `json:"id"`
	ChannelID          string     `json:"channel_id"`
	ChannelName        string     `json:"channel_name"`
	GroupID            string     `json:"group_id"`
	UserID             string     `json:"user_id"`
	CreatedAt          int64      `json:"created_at"`
	Status             string     `json:"status"`
	ModelName          string     `json:"model_name"`
	PromptTokens       int        `json:"prompt_tokens"`
	CompletionTokens   int        `json:"completion_tokens"`
	UseTime            int        `json:"use_time"`
	IsStream           bool       `json:"is_stream"`
	RequestID          string     `json:"request_id"`
	ConsumerAmount     int64      `json:"consumer_amount"`
	OwnerIncome        int64      `json:"owner_income"`
	PlatformCommission int64      `json:"platform_commission"`
	Multiplier         float64    `json:"multiplier"`
	IncomeStatus       string     `json:"income_status"`
	AvailableAt        *time.Time `json:"available_at,omitempty"`
	ReleasedAt         *time.Time `json:"released_at,omitempty"`
}

type OwnerUsageLogResult struct {
	Items    []OwnerUsageLogItem  `json:"items"`
	Summary  OwnerUsageLogSummary `json:"summary"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

type OwnerUsageLogSummary struct {
	RequestCount   int64 `json:"request_count"`
	SuccessCount   int64 `json:"success_count"`
	FailedCount    int64 `json:"failed_count"`
	ConsumerAmount int64 `json:"consumer_amount"`
	OwnerIncome    int64 `json:"owner_income"`
}
