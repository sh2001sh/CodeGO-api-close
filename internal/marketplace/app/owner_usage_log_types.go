package app

import "time"

type OwnerUsageLogQuery struct {
	ChannelID         string
	Status            string
	ModelName         string
	RequestID         string
	UpstreamRequestID string
	ExternalUserID    string
	Search            string
	StartTimestamp    int64
	EndTimestamp      int64
	Page              int
	PageSize          int
	SummaryOnly       bool
	userFilterIDs     []int
	searchUserIDs     []int
}

type OwnerUsageLogItem struct {
	ID                 int                    `json:"id"`
	ChannelID          string                 `json:"channel_id"`
	ChannelName        string                 `json:"channel_name"`
	GroupID            string                 `json:"group_id"`
	UserID             string                 `json:"user_id"`
	CreatedAt          int64                  `json:"created_at"`
	Status             string                 `json:"status"`
	ModelName          string                 `json:"model_name"`
	PromptTokens       int                    `json:"prompt_tokens"`
	CompletionTokens   int                    `json:"completion_tokens"`
	CacheReadTokens    int64                  `json:"cache_read_tokens"`
	CacheWriteTokens   int64                  `json:"cache_write_tokens"`
	UseTime            int                    `json:"use_time"`
	IsStream           bool                   `json:"is_stream"`
	RequestID          string                 `json:"request_id"`
	UpstreamRequestID  string                 `json:"upstream_request_id"`
	FirstByteMs        int64                  `json:"first_byte_ms"`
	AttemptTTFTMs      int64                  `json:"attempt_ttft_ms"`
	TotalDurationMs    int64                  `json:"total_duration_ms"`
	StatusCode         int                    `json:"status_code"`
	ErrorType          string                 `json:"error_type"`
	ErrorCode          string                 `json:"error_code"`
	ErrorMessage       string                 `json:"error_message"`
	RequestPath        string                 `json:"request_path"`
	RetryCount         int                    `json:"retry_count"`
	FirstByteTrace     map[string]interface{} `json:"first_byte_trace,omitempty"`
	ConsumerAmount     int64                  `json:"consumer_amount"`
	OwnerIncome        int64                  `json:"owner_income"`
	ReclaimedIncome    int64                  `json:"reclaimed_income"`
	PlatformCommission int64                  `json:"platform_commission"`
	Multiplier         float64                `json:"multiplier"`
	IncomeStatus       string                 `json:"income_status"`
	AvailableAt        *time.Time             `json:"available_at,omitempty"`
	ReleasedAt         *time.Time             `json:"released_at,omitempty"`
	IncomeBreakdown    *OwnerIncomeBreakdown  `json:"income_breakdown,omitempty"`
}

// Unknown historical fields stay null; current plan configuration is never a
// source for a past request's identity, amounts or rates.
type OwnerIncomeBreakdown struct {
	AlgorithmVersion       *string  `json:"algorithm_version"`
	SnapshotStatus         string   `json:"snapshot_status"`
	BillingSource          string   `json:"billing_source"`
	SubscriptionID         *int     `json:"subscription_id"`
	SubscriptionPlanID     *int     `json:"subscription_plan_id"`
	SubscriptionPlanTitle  *string  `json:"subscription_plan_title"`
	SubscriptionType       *string  `json:"subscription_type"`
	SettlementGrossAmount  *int64   `json:"settlement_gross_amount"`
	BaseCommissionAmount   *int64   `json:"base_commission_amount"`
	BaseCommissionRateBPS  *int64   `json:"base_commission_rate_bps"`
	PackageBonusRate       *float64 `json:"package_bonus_rate"`
	ChannelShareBPS        *int64   `json:"channel_share_bps"`
	PackageSurchargeRate   *float64 `json:"package_surcharge_rate"`
	PackageSurchargeAmount *int64   `json:"package_surcharge_amount"`
	ReferralRebateAmount   *int64   `json:"referral_rebate_amount"`
	OwnerNetAmount         *int64   `json:"owner_net_amount"`
	Formula                *string  `json:"formula"`
}

type OwnerUsageLogResult struct {
	Items    []OwnerUsageLogItem  `json:"items"`
	Summary  OwnerUsageLogSummary `json:"summary"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

type OwnerUsageLogSummary struct {
	RequestCount         int64 `json:"request_count"`
	SuccessCount         int64 `json:"success_count"`
	FailedCount          int64 `json:"failed_count"`
	UpstreamAttemptCount int64 `json:"upstream_attempt_count"`
	UpstreamSuccessCount int64 `json:"upstream_success_count"`
	UpstreamFailedCount  int64 `json:"upstream_failed_count"`
	ConsumerAmount       int64 `json:"consumer_amount"`
	OwnerIncome          int64 `json:"owner_income"`
	PendingIncome        int64 `json:"pending_income"`
	ReleasedIncome       int64 `json:"released_income"`
	ReclaimedIncome      int64 `json:"reclaimed_income"`
}
