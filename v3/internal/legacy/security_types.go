package legacy

import "time"

// Nullable persistent fields remain nullable; historical references may point
// to deleted users/channels and are not invented foreign-key requirements.
type sourceSecurityAudit struct {
	ID                   string     `json:"id"`
	DedupeKey            string     `json:"dedupe_key"`
	RequestID            *string    `json:"request_id"`
	Source               string     `json:"source"`
	Decision             string     `json:"decision"`
	RiskCode             string     `json:"risk_code"`
	Severity             string     `json:"severity"`
	UserID               *int64     `json:"user_id"`
	TokenID              *int64     `json:"token_id"`
	TokenName            *string    `json:"token_name"`
	ChannelID            *int64     `json:"channel_id"`
	MarketplaceChannelID *string    `json:"marketplace_channel_id"`
	MarketplaceGroupID   *string    `json:"marketplace_group_id"`
	OwnerUserID          *int64     `json:"owner_user_id"`
	Model                *string    `json:"model"`
	Protocol             *string    `json:"protocol"`
	HTTPStatus           *int32     `json:"http_status"`
	UpstreamErrorType    *string    `json:"upstream_error_type"`
	UpstreamErrorCode    *string    `json:"upstream_error_code"`
	UpstreamErrorMessage *string    `json:"upstream_error_message"`
	UpstreamErrorBody    *string    `json:"upstream_error_body"`
	PromptHash           *string    `json:"prompt_hash"`
	PromptPreview        *string    `json:"prompt_preview"`
	PromptLength         *int32     `json:"prompt_length"`
	MessageCount         *int32     `json:"message_count"`
	BillingResult        *string    `json:"billing_result"`
	NotificationStatus   *string    `json:"notification_status"`
	NotificationTargets  int32      `json:"notification_targets"`
	NotificationSuccess  int32      `json:"notification_success"`
	NotifiedAt           *time.Time `json:"notified_at"`
	ReviewStatus         string     `json:"review_status"`
	ReviewNote           *string    `json:"review_note"`
	ReviewedBy           *int64     `json:"reviewed_by"`
	ReviewedAt           *time.Time `json:"reviewed_at"`
	CreatedAt            *time.Time `json:"created_at"`
	UpdatedAt            *time.Time `json:"updated_at"`
}

func (a sourceSecurityAudit) projection() map[string]any {
	return map[string]any{
		"id": a.ID, "dedupe_key": a.DedupeKey, "request_id": a.RequestID, "source": a.Source,
		"decision": a.Decision, "risk_code": a.RiskCode, "severity": a.Severity,
		"user_id": a.UserID, "token_id": a.TokenID, "token_name": a.TokenName, "channel_id": a.ChannelID,
		"marketplace_channel_id": a.MarketplaceChannelID, "marketplace_group_id": a.MarketplaceGroupID,
		"owner_user_id": a.OwnerUserID, "model": a.Model, "protocol": a.Protocol, "http_status": a.HTTPStatus,
		"upstream_error_type": a.UpstreamErrorType, "upstream_error_code": a.UpstreamErrorCode,
		"upstream_error_message": a.UpstreamErrorMessage, "upstream_error_body": a.UpstreamErrorBody,
		"prompt_hash": a.PromptHash, "prompt_preview": a.PromptPreview, "prompt_length": a.PromptLength,
		"message_count": a.MessageCount, "billing_result": a.BillingResult, "notification_status": a.NotificationStatus,
		"notification_targets": a.NotificationTargets, "notification_success": a.NotificationSuccess,
		"notified_at": a.NotifiedAt, "review_status": a.ReviewStatus, "review_note": a.ReviewNote,
		"reviewed_by": a.ReviewedBy, "reviewed_at": a.ReviewedAt, "created_at": a.CreatedAt, "updated_at": a.UpdatedAt,
	}
}

type sourceSecurityState struct {
	UserID          int64   `json:"user_id"`
	Strikes         int32   `json:"strikes"`
	RestrictedUntil int64   `json:"restricted_until"`
	LastWindowEnd   int64   `json:"last_window_end"`
	Blocked         bool    `json:"blocked"`
	Evidence        *string `json:"evidence"`
	UpdatedAt       *int64  `json:"updated_at"`
}

func (s sourceSecurityState) projection() map[string]any {
	return map[string]any{"user_id": s.UserID, "strikes": s.Strikes, "restricted_until": s.RestrictedUntil,
		"last_window_end": s.LastWindowEnd, "blocked": s.Blocked, "evidence": s.Evidence, "updated_at": s.UpdatedAt}
}
