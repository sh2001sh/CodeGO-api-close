package schema

import "time"

// SecurityAuditEvent is the durable evidence row for a platform guard decision
// or an upstream cyber-policy rejection. Request prompts are never stored raw.
type SecurityAuditEvent struct {
	ID                   string     `json:"id" gorm:"column:id;primaryKey;size:64"`
	DedupeKey            string     `json:"-" gorm:"column:dedupe_key;size:64;uniqueIndex;not null"`
	RequestID            string     `json:"request_id" gorm:"column:request_id;size:128;index"`
	Source               string     `json:"source" gorm:"column:source;size:32;index;not null"`
	Decision             string     `json:"decision" gorm:"column:decision;size:24;index;not null"`
	RiskCode             string     `json:"risk_code" gorm:"column:risk_code;size:64;index;not null"`
	Severity             string     `json:"severity" gorm:"column:severity;size:16;index;not null"`
	UserID               int        `json:"user_id" gorm:"column:user_id;index"`
	TokenID              int        `json:"token_id" gorm:"column:token_id;index"`
	TokenName            string     `json:"token_name" gorm:"column:token_name;size:128"`
	ChannelID            int        `json:"channel_id" gorm:"column:channel_id;index"`
	MarketplaceChannelID string     `json:"marketplace_channel_id" gorm:"column:marketplace_channel_id;size:64;index"`
	MarketplaceGroupID   string     `json:"marketplace_group_id" gorm:"column:marketplace_group_id;size:64;index"`
	OwnerUserID          int        `json:"owner_user_id" gorm:"column:owner_user_id;index"`
	Model                string     `json:"model" gorm:"column:model;size:191;index"`
	Protocol             string     `json:"protocol" gorm:"column:protocol;size:96;index"`
	HTTPStatus           int        `json:"http_status" gorm:"column:http_status"`
	UpstreamErrorType    string     `json:"upstream_error_type" gorm:"column:upstream_error_type;size:64"`
	UpstreamErrorCode    string     `json:"upstream_error_code" gorm:"column:upstream_error_code;size:64"`
	UpstreamErrorMessage string     `json:"upstream_error_message" gorm:"column:upstream_error_message;type:text"`
	UpstreamErrorBody    string     `json:"upstream_error_body" gorm:"column:upstream_error_body;type:text"`
	PromptHash           string     `json:"prompt_hash" gorm:"column:prompt_hash;size:64;index"`
	PromptPreview        string     `json:"prompt_preview" gorm:"column:prompt_preview;size:512"`
	PromptLength         int        `json:"prompt_length" gorm:"column:prompt_length"`
	MessageCount         int        `json:"message_count" gorm:"column:message_count"`
	BillingResult        string     `json:"billing_result" gorm:"column:billing_result;size:24;index"`
	NotificationStatus   string     `json:"notification_status" gorm:"column:notification_status;size:24;index"`
	NotificationTargets  int        `json:"notification_targets" gorm:"column:notification_targets;not null;default:0"`
	NotificationSuccess  int        `json:"notification_success" gorm:"column:notification_success;not null;default:0"`
	NotifiedAt           *time.Time `json:"notified_at" gorm:"column:notified_at"`
	ReviewStatus         string     `json:"review_status" gorm:"column:review_status;size:24;index;not null"`
	ReviewNote           string     `json:"review_note" gorm:"column:review_note;size:1000"`
	ReviewedBy           int        `json:"reviewed_by" gorm:"column:reviewed_by;index"`
	ReviewedAt           *time.Time `json:"reviewed_at" gorm:"column:reviewed_at"`
	CreatedAt            time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime;index"`
	UpdatedAt            time.Time  `json:"updated_at" gorm:"column:updated_at;autoCreateTime;autoUpdateTime"`
	RecentTriggerCount   int64      `json:"recent_trigger_count" gorm:"-"`
}

func (SecurityAuditEvent) TableName() string { return "security_audit_events" }
