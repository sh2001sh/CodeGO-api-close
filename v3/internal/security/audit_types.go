package security

import "time"

type Actor struct {
	UserID int64
	Admin  bool
}
type Query struct {
	Page               int
	PageSize           int
	Source             string
	ReviewStatus       string
	MarketplaceChannel string
	Model              string
	Search             string
	StartTimestamp     int64
	EndTimestamp       int64
}
type Event struct {
	ID                   string     `json:"id"`
	RequestID            string     `json:"request_id"`
	Source               string     `json:"source"`
	Decision             string     `json:"decision"`
	RiskCode             string     `json:"risk_code"`
	Severity             string     `json:"severity"`
	UserID               int64      `json:"user_id,string"`
	TokenID              int64      `json:"token_id,string"`
	TokenName            string     `json:"token_name"`
	ChannelID            int64      `json:"channel_id,string"`
	MarketplaceChannelID string     `json:"marketplace_channel_id"`
	MarketplaceGroupID   string     `json:"marketplace_group_id"`
	OwnerUserID          int64      `json:"owner_user_id,string"`
	Model                string     `json:"model"`
	Protocol             string     `json:"protocol"`
	HTTPStatus           int        `json:"http_status"`
	UpstreamErrorType    string     `json:"upstream_error_type"`
	UpstreamErrorCode    string     `json:"upstream_error_code"`
	UpstreamErrorMessage string     `json:"upstream_error_message"`
	UpstreamErrorBody    string     `json:"upstream_error_body"`
	PromptHash           string     `json:"prompt_hash"`
	PromptPreview        string     `json:"prompt_preview"`
	PromptLength         int        `json:"prompt_length"`
	MessageCount         int        `json:"message_count"`
	BillingResult        string     `json:"billing_result"`
	NotificationStatus   string     `json:"notification_status"`
	NotificationTargets  int        `json:"notification_targets"`
	NotificationSuccess  int        `json:"notification_success"`
	NotifiedAt           *time.Time `json:"notified_at"`
	ReviewStatus         string     `json:"review_status"`
	ReviewNote           string     `json:"review_note"`
	ReviewedBy           int64      `json:"reviewed_by,string"`
	ReviewedAt           *time.Time `json:"reviewed_at"`
	CreatedAt            *time.Time `json:"created_at"`
	UpdatedAt            *time.Time `json:"updated_at"`
}
type EventList struct {
	Items    []Event `json:"items"`
	Total    int64   `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

const eventSelect = `id,coalesce(request_id,''),source,decision,risk_code,severity,
 coalesce(user_id,0),coalesce(token_id,0),coalesce(token_name,''),coalesce(channel_id,0),
 coalesce(marketplace_channel_id,''),coalesce(marketplace_group_id,''),coalesce(owner_user_id,0),
 coalesce(model,''),coalesce(protocol,''),coalesce(http_status,0),
 coalesce(upstream_error_type,''),coalesce(upstream_error_code,''),coalesce(upstream_error_message,''),coalesce(upstream_error_body,''),
 coalesce(prompt_hash,''),coalesce(prompt_preview,''),coalesce(prompt_length,0),coalesce(message_count,0),
 coalesce(billing_result,''),coalesce(notification_status,''),notification_targets,notification_success,notified_at,
 review_status,coalesce(review_note,''),coalesce(reviewed_by,0),reviewed_at,created_at,updated_at`

func scanEvent(row interface{ Scan(...any) error }, admin bool) (Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.RequestID, &e.Source, &e.Decision, &e.RiskCode, &e.Severity,
		&e.UserID, &e.TokenID, &e.TokenName, &e.ChannelID, &e.MarketplaceChannelID, &e.MarketplaceGroupID, &e.OwnerUserID,
		&e.Model, &e.Protocol, &e.HTTPStatus, &e.UpstreamErrorType, &e.UpstreamErrorCode, &e.UpstreamErrorMessage, &e.UpstreamErrorBody,
		&e.PromptHash, &e.PromptPreview, &e.PromptLength, &e.MessageCount, &e.BillingResult, &e.NotificationStatus,
		&e.NotificationTargets, &e.NotificationSuccess, &e.NotifiedAt, &e.ReviewStatus, &e.ReviewNote, &e.ReviewedBy, &e.ReviewedAt, &e.CreatedAt, &e.UpdatedAt)
	if !admin {
		e.UpstreamErrorBody = ""
		e.UpstreamErrorMessage = ""
		e.PromptPreview = ""
	}
	return e, err
}
