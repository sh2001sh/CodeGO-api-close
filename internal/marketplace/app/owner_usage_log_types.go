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
	PlatformCommission int64                  `json:"platform_commission"`
	Multiplier         float64                `json:"multiplier"`
	IncomeStatus       string                 `json:"income_status"`
	AvailableAt        *time.Time             `json:"available_at,omitempty"`
	ReleasedAt         *time.Time             `json:"released_at,omitempty"`
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
