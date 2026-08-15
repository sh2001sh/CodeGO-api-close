package app

import "time"

type CreateChannelRequest struct {
	ProviderType      string   `json:"provider_type"`
	SourceLabel       string   `json:"source_label"`
	BaseURL           string   `json:"base_url"`
	APIKey            string   `json:"api_key"`
	DeclaredModels    []string `json:"declared_models"`
	Multiplier        float64  `json:"multiplier"`
	Visibility        string   `json:"visibility"`
	MaxConcurrency    int      `json:"max_concurrency"`
	QPS               float64  `json:"qps"`
	MaintenanceWindow string   `json:"maintenance_window"`
}

type UpdateChannelRequest struct {
	ProviderType      *string   `json:"provider_type"`
	DeclaredModels    *[]string `json:"declared_models"`
	Multiplier        *float64  `json:"multiplier"`
	Visibility        *string   `json:"visibility"`
	MaxConcurrency    *int      `json:"max_concurrency"`
	QPS               *float64  `json:"qps"`
	MaintenanceWindow *string   `json:"maintenance_window"`
	BaseURL           *string   `json:"base_url"`
	APIKey            *string   `json:"api_key"`
	SourceLabel       *string   `json:"source_label"`
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

type ChannelView struct {
	ID                          string                    `json:"id"`
	GroupID                     string                    `json:"group_id"`
	PublicSlug                  string                    `json:"public_slug"`
	SystemDisplayName           string                    `json:"system_display_name"`
	ProviderType                string                    `json:"provider_type"`
	SubmittedSourceLabel        string                    `json:"submitted_source_label"`
	ApprovedSourceLabel         string                    `json:"approved_source_label"`
	SourceLabelStatus           string                    `json:"source_label_status"`
	SourceLabelReviewReason     string                    `json:"source_label_review_reason"`
	CredentialTail              string                    `json:"credential_tail"`
	CredentialVersion           int                       `json:"credential_version"`
	DeclaredModels              []string                  `json:"declared_models"`
	ModelVerificationResults    []ModelVerificationResult `json:"model_verification_results"`
	ModelConsistencyStatus      string                    `json:"model_consistency_status"`
	Multiplier                  float64                   `json:"multiplier"`
	LifecycleStatus             string                    `json:"lifecycle_status"`
	VerificationStatus          string                    `json:"verification_status"`
	VerificationStage           string                    `json:"verification_stage"`
	VerificationSummary         string                    `json:"verification_summary"`
	VerificationDetectorVersion string                    `json:"verification_detector_version"`
	VerificationStartedAt       *time.Time                `json:"verification_started_at"`
	VerificationCompletedAt     *time.Time                `json:"verification_completed_at"`
	Visibility                  string                    `json:"visibility"`
	MaxConcurrency              int                       `json:"max_concurrency"`
	QPS                         float64                   `json:"qps"`
	MaintenanceWindow           string                    `json:"maintenance_window"`
	InternalChannelID           *int                      `json:"internal_channel_id"`
	LastReviewReason            string                    `json:"last_review_reason"`
	VerificationDueAt           *time.Time                `json:"verification_due_at"`
	RequestCount                int64                     `json:"request_count"`
	TotalIncome                 int64                     `json:"total_income"`
	PendingIncome               int64                     `json:"pending_income"`
	ReleasedIncome              int64                     `json:"released_income"`
	CreatedAt                   time.Time                 `json:"created_at"`
	UpdatedAt                   time.Time                 `json:"updated_at"`
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
	ID                       string                    `json:"id"`
	ChannelID                string                    `json:"channel_id"`
	PublicSlug               string                    `json:"public_slug"`
	SystemDisplayName        string                    `json:"system_display_name"`
	SourceType               string                    `json:"source_type"`
	SourceLabel              string                    `json:"source_label"`
	ProviderType             string                    `json:"provider_type"`
	CreditPoolPolicy         string                    `json:"credit_pool_policy"`
	LifecycleStatus          string                    `json:"lifecycle_status"`
	VerificationStatus       string                    `json:"verification_status"`
	VerificationDueAt        *time.Time                `json:"verification_due_at"`
	Multiplier               float64                   `json:"multiplier"`
	Models                   []string                  `json:"models"`
	ModelVerificationResults []ModelVerificationResult `json:"model_verification_results"`
	ModelConsistencyStatus   string                    `json:"model_consistency_status"`
	Rank                     int                       `json:"rank"`
	Score                    float64                   `json:"score"`
	SuccessRate              float64                   `json:"success_rate"`
	WilsonSuccessRate        float64                   `json:"wilson_success_rate"`
	AvgTTFTMs                float64                   `json:"avg_ttft_ms"`
	AvgLatencyMs             float64                   `json:"avg_latency_ms"`
	AvgTPS                   float64                   `json:"avg_tps"`
	RequestCount             int64                     `json:"request_count"`
	IndependentConsumers     int64                     `json:"independent_consumers"`
	Observing                bool                      `json:"observing"`
	UpdatedAt                time.Time                 `json:"updated_at"`
}

type GroupListResult struct {
	Items       []GroupListItem `json:"items"`
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
	GroupID          string
	InternalGroup    string
	OwnerUserID      int
	SourceType       string
	CreditPoolPolicy string
	Multiplier       float64
}

type AutoRoutePoolUpdateRequest struct {
	GroupIDs []string `json:"group_ids"`
}

type AutoRoutePoolItem struct {
	GroupID           string   `json:"group_id"`
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
	ChannelID string
	Page      int
	PageSize  int
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
