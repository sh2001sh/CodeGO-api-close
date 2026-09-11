package securityaudit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/platform/notifyx"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	EventSourcePromptGuard         = "prompt_guard"
	EventSourceUpstreamCyberPolicy = "upstream_cyber_policy"
	ReviewStatusUnreviewed         = "unreviewed"
	ReviewStatusAcknowledged       = "acknowledged"
	ReviewStatusResolved           = "resolved"
	ReviewStatusFalsePositive      = "false_positive"
	BillingResultNotCharged        = "not_charged"
	NotificationStatusPending      = "pending"
	NotificationStatusDispatched   = "dispatched"
	NotificationStatusPartial      = "partial_failed"
	NotificationStatusFailed       = "failed"
	NotificationStatusSkipped      = "skipped"
	MaxEventExportRows             = 20000
)

type EventInput struct {
	RequestID            string
	Source               string
	Decision             string
	RiskCode             string
	Severity             string
	UserID               int
	TokenID              int
	TokenName            string
	ChannelID            int
	MarketplaceGroupID   string
	OwnerUserID          int
	Model                string
	Protocol             string
	HTTPStatus           int
	UpstreamErrorType    string
	UpstreamErrorCode    string
	UpstreamErrorMessage string
	UpstreamErrorBody    []byte
	PromptBody           []byte
	PromptFallback       string
	BillingResult        string
}

type EventQuery struct {
	ViewerUserID       int
	Admin              bool
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

type EventSummary struct {
	Total            int64 `json:"total"`
	Unreviewed       int64 `json:"unreviewed"`
	AffectedChannels int64 `json:"affected_channels"`
	AffectedUsers    int64 `json:"affected_users"`
	Today            int64 `json:"today"`
}

type EventList struct {
	Items    []gatewayschema.SecurityAuditEvent `json:"items"`
	Total    int64                              `json:"total"`
	Page     int                                `json:"page"`
	PageSize int                                `json:"page_size"`
	Summary  EventSummary                       `json:"summary"`
}

func RecordEvent(ctx context.Context, input EventInput) (*gatewayschema.SecurityAuditEvent, error) {
	if platformdb.DB == nil {
		return nil, errors.New("security audit database is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	input.Source = strings.TrimSpace(input.Source)
	input.RiskCode = strings.TrimSpace(input.RiskCode)
	if input.Source == "" || input.RiskCode == "" {
		return nil, errors.New("security audit source and risk code are required")
	}
	if input.Decision == "" {
		input.Decision = "blocked"
	}
	input.Severity = normalizeSeverity(input.Severity)
	if input.BillingResult == "" {
		input.BillingResult = BillingResultNotCharged
	}

	marketplaceChannelID := ""
	if input.MarketplaceGroupID != "" {
		var group marketplaceschema.Group
		if err := platformdb.DB.WithContext(ctx).Select("channel_id", "owner_user_id").Where("id = ?", input.MarketplaceGroupID).First(&group).Error; err == nil {
			marketplaceChannelID = group.ChannelID
			if input.OwnerUserID == 0 {
				input.OwnerUserID = group.OwnerUserID
			}
		}
	}

	snapshot, _ := ExtractSnapshot(Request{
		RequestID: input.RequestID, Group: input.MarketplaceGroupID, Protocol: input.Protocol,
		Model: input.Model, Body: input.PromptBody, FallbackText: input.PromptFallback, Stage: "security_event",
	}, true)
	body := truncateRunes(strings.TrimSpace(string(input.UpstreamErrorBody)), 4096)
	dedupe := eventDedupeKey(input, body, snapshot.PromptHash)
	event := &gatewayschema.SecurityAuditEvent{
		ID: platformruntime.GetUUID(), DedupeKey: dedupe, RequestID: input.RequestID,
		Source: input.Source, Decision: input.Decision, RiskCode: input.RiskCode, Severity: input.Severity,
		UserID: input.UserID, TokenID: input.TokenID, TokenName: input.TokenName, ChannelID: input.ChannelID,
		MarketplaceChannelID: marketplaceChannelID, MarketplaceGroupID: input.MarketplaceGroupID, OwnerUserID: input.OwnerUserID,
		Model: input.Model, Protocol: input.Protocol, HTTPStatus: input.HTTPStatus,
		UpstreamErrorType: input.UpstreamErrorType, UpstreamErrorCode: input.UpstreamErrorCode,
		UpstreamErrorMessage: truncateRunes(input.UpstreamErrorMessage, 2000), UpstreamErrorBody: body,
		PromptHash: snapshot.PromptHash, PromptPreview: snapshot.RedactedPreview,
		PromptLength: snapshot.PromptLength, MessageCount: snapshot.MessageCount,
		BillingResult: input.BillingResult, NotificationStatus: NotificationStatusPending,
		ReviewStatus: ReviewStatusUnreviewed,
	}
	result := platformdb.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(event)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		var existing gatewayschema.SecurityAuditEvent
		if err := platformdb.DB.WithContext(ctx).Where("dedupe_key = ?", dedupe).First(&existing).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	notifySecurityEvent(*event)
	return event, nil
}

func eventDedupeKey(input EventInput, body, promptHash string) string {
	value := strings.Join([]string{
		input.RequestID, input.Source, fmt.Sprint(input.ChannelID), input.MarketplaceGroupID,
		input.RiskCode, body, promptHash,
	}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func ListEvents(query EventQuery) (*EventList, error) {
	if platformdb.DB == nil {
		return nil, errors.New("security audit database is not initialized")
	}
	query.Page = max(query.Page, 1)
	query.PageSize = min(max(query.PageSize, 1), 100)
	base, err := scopedEventQuery(query)
	if err != nil {
		return nil, err
	}
	base = applyEventFilters(base, query)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, err
	}
	items := make([]gatewayschema.SecurityAuditEvent, 0)
	if err := base.Order("created_at DESC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Find(&items).Error; err != nil {
		return nil, err
	}
	if err := populateRecentTriggerCounts(query, items); err != nil {
		return nil, err
	}
	summary, err := eventSummary(query)
	if err != nil {
		return nil, err
	}
	return &EventList{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize, Summary: summary}, nil
}

// ExportEvents returns all events matching the viewer-scoped filters. The
// explicit limit prevents a broad export from holding the API process for an
// unbounded amount of time; callers must narrow the filters when it is hit.
func ExportEvents(query EventQuery) ([]gatewayschema.SecurityAuditEvent, error) {
	if platformdb.DB == nil {
		return nil, errors.New("security audit database is not initialized")
	}
	base, err := scopedEventQuery(query)
	if err != nil {
		return nil, err
	}
	base = applyEventFilters(base, query)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, err
	}
	if total > MaxEventExportRows {
		return nil, fmt.Errorf("导出日志超过 %d 条，请缩小时间范围或增加筛选条件", MaxEventExportRows)
	}
	items := make([]gatewayschema.SecurityAuditEvent, 0, total)
	if err := base.Order("created_at DESC").Limit(MaxEventExportRows).Find(&items).Error; err != nil {
		return nil, err
	}
	if err := populateRecentTriggerCounts(query, items); err != nil {
		return nil, err
	}
	return items, nil
}

func UpdateEventReview(viewerUserID int, admin bool, eventID, status, note string) (*gatewayschema.SecurityAuditEvent, error) {
	status = strings.TrimSpace(status)
	switch status {
	case ReviewStatusUnreviewed, ReviewStatusAcknowledged, ReviewStatusResolved, ReviewStatusFalsePositive:
	default:
		return nil, errors.New("invalid security audit review status")
	}
	query, err := scopedEventQuery(EventQuery{ViewerUserID: viewerUserID, Admin: admin})
	if err != nil {
		return nil, err
	}
	var event gatewayschema.SecurityAuditEvent
	if err := query.Where("id = ?", strings.TrimSpace(eventID)).First(&event).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	updates := map[string]any{"review_status": status, "review_note": truncateRunes(note, 1000), "reviewed_by": viewerUserID, "reviewed_at": &now}
	if status == ReviewStatusUnreviewed {
		updates["reviewed_by"] = 0
		updates["reviewed_at"] = nil
	}
	if err := platformdb.DB.Model(&event).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := platformdb.DB.Where("id = ?", event.ID).First(&event).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

func scopedEventQuery(query EventQuery) (*gorm.DB, error) {
	if query.Admin {
		return platformdb.DB.Model(&gatewayschema.SecurityAuditEvent{}), nil
	}
	if query.ViewerUserID <= 0 {
		return nil, errors.New("security audit viewer is invalid")
	}
	return platformdb.DB.Model(&gatewayschema.SecurityAuditEvent{}).Where("owner_user_id = ?", query.ViewerUserID), nil
}

func applyEventFilters(db *gorm.DB, query EventQuery) *gorm.DB {
	if value := strings.TrimSpace(query.Source); value != "" {
		db = db.Where("source = ?", value)
	}
	if value := strings.TrimSpace(query.ReviewStatus); value != "" {
		db = db.Where("review_status = ?", value)
	}
	if value := strings.TrimSpace(query.MarketplaceChannel); value != "" {
		db = db.Where("marketplace_channel_id = ?", value)
	}
	if value := strings.TrimSpace(query.Model); value != "" {
		db = db.Where("model = ?", value)
	}
	if query.StartTimestamp > 0 {
		db = db.Where("created_at >= ?", time.Unix(query.StartTimestamp, 0))
	}
	if query.EndTimestamp > 0 {
		db = db.Where("created_at <= ?", time.Unix(query.EndTimestamp, 0))
	}
	if value := strings.TrimSpace(query.Search); value != "" {
		like := "%" + value + "%"
		if userID, err := strconv.Atoi(value); err == nil {
			db = db.Where("(request_id LIKE ? OR token_name LIKE ? OR upstream_error_message LIKE ? OR user_id = ?)", like, like, like, userID)
		} else {
			db = db.Where("(request_id LIKE ? OR token_name LIKE ? OR upstream_error_message LIKE ?)", like, like, like)
		}
	}
	return db
}

func eventSummary(query EventQuery) (EventSummary, error) {
	summary := EventSummary{}
	base := func() (*gorm.DB, error) {
		db, err := scopedEventQuery(query)
		if err != nil {
			return nil, err
		}
		return applyEventFilters(db, query), nil
	}
	db, err := base()
	if err != nil {
		return summary, err
	}
	if err := db.Count(&summary.Total).Error; err != nil {
		return summary, err
	}
	db, _ = base()
	if err := db.Where("review_status = ?", ReviewStatusUnreviewed).Count(&summary.Unreviewed).Error; err != nil {
		return summary, err
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	db, _ = base()
	if err := db.Where("created_at >= ?", today).Count(&summary.Today).Error; err != nil {
		return summary, err
	}
	db, _ = base()
	if err := db.Where("marketplace_channel_id <> ''").Distinct("marketplace_channel_id").Count(&summary.AffectedChannels).Error; err != nil {
		return summary, err
	}
	db, _ = base()
	if err := db.Where("user_id > 0").Distinct("user_id").Count(&summary.AffectedUsers).Error; err != nil {
		return summary, err
	}
	return summary, nil
}

func populateRecentTriggerCounts(query EventQuery, events []gatewayschema.SecurityAuditEvent) error {
	if len(events) == 0 {
		return nil
	}
	tokenIDs := make([]int, 0, len(events))
	userIDs := make([]int, 0, len(events))
	seenTokens := make(map[int]struct{})
	seenUsers := make(map[int]struct{})
	for _, event := range events {
		if event.TokenID > 0 {
			if _, ok := seenTokens[event.TokenID]; !ok {
				seenTokens[event.TokenID] = struct{}{}
				tokenIDs = append(tokenIDs, event.TokenID)
			}
		} else if event.UserID > 0 {
			if _, ok := seenUsers[event.UserID]; !ok {
				seenUsers[event.UserID] = struct{}{}
				userIDs = append(userIDs, event.UserID)
			}
		}
	}
	db, err := scopedEventQuery(query)
	if err != nil {
		return err
	}
	db = db.Where("created_at >= ?", time.Now().Add(-24*time.Hour))
	switch {
	case len(tokenIDs) > 0 && len(userIDs) > 0:
		db = db.Where("token_id IN ? OR (token_id = 0 AND user_id IN ?)", tokenIDs, userIDs)
	case len(tokenIDs) > 0:
		db = db.Where("token_id IN ?", tokenIDs)
	case len(userIDs) > 0:
		db = db.Where("token_id = 0 AND user_id IN ?", userIDs)
	default:
		return nil
	}
	type triggerCount struct {
		UserID  int
		TokenID int
		Count   int64
	}
	counts := make([]triggerCount, 0)
	if err := db.Select("user_id, token_id, COUNT(*) AS count").Group("user_id, token_id").Scan(&counts).Error; err != nil {
		return err
	}
	byToken := make(map[int]int64, len(counts))
	byUser := make(map[int]int64, len(counts))
	for _, count := range counts {
		if count.TokenID > 0 {
			byToken[count.TokenID] += count.Count
		} else if count.UserID > 0 {
			byUser[count.UserID] += count.Count
		}
	}
	for index := range events {
		if events[index].TokenID > 0 {
			events[index].RecentTriggerCount = byToken[events[index].TokenID]
		} else {
			events[index].RecentTriggerCount = byUser[events[index].UserID]
		}
	}
	return nil
}

func notifySecurityEvent(event gatewayschema.SecurityAuditEvent) {
	gopool.Go(func() {
		recipients := make(map[int]identityschema.User)
		var users []identityschema.User
		query := platformdb.DB.Select("id", "email", "setting", "role")
		if event.OwnerUserID > 0 {
			query = query.Where("id = ? OR role >= ?", event.OwnerUserID, constant.RoleAdminUser)
		} else {
			query = query.Where("role >= ?", constant.RoleAdminUser)
		}
		if err := query.Find(&users).Error; err != nil {
			platformobservability.SysError("load security audit notification recipients failed: " + err.Error())
			updateNotificationDelivery(event.ID, NotificationStatusFailed, 0, 0, nil)
			return
		}
		for _, user := range users {
			recipients[user.Id] = user
		}
		subject := "安全审计发现高风险请求"
		content := fmt.Sprintf("事件 %s 于 %s 触发，模型：%s，渠道：%s，风险：%s。请进入安全审计页面查看。",
			event.ID, event.CreatedAt.Format("2006-01-02 15:04:05"), event.Model, defaultText(event.MarketplaceChannelID, fmt.Sprint(event.ChannelID)), event.RiskCode)
		success := 0
		for _, user := range recipients {
			if err := notifyx.NotifyUser(user.Id, user.Email, identitydomain.GetSetting(&user), dto.NewNotify("security_audit", subject, content, nil)); err != nil {
				platformobservability.SysError(fmt.Sprintf("notify security audit recipient %d failed: %s", user.Id, err.Error()))
				continue
			}
			success++
		}
		targets := len(recipients)
		status := NotificationStatusDispatched
		switch {
		case targets == 0:
			status = NotificationStatusSkipped
		case success == 0:
			status = NotificationStatusFailed
		case success < targets:
			status = NotificationStatusPartial
		}
		var notifiedAt *time.Time
		if success > 0 {
			now := time.Now()
			notifiedAt = &now
		}
		updateNotificationDelivery(event.ID, status, targets, success, notifiedAt)
	})
}

func updateNotificationDelivery(eventID, status string, targets, success int, notifiedAt *time.Time) {
	updates := map[string]any{
		"notification_status":  status,
		"notification_targets": targets,
		"notification_success": success,
		"notified_at":          notifiedAt,
	}
	if err := platformdb.DB.Model(&gatewayschema.SecurityAuditEvent{}).Where("id = ?", eventID).Updates(updates).Error; err != nil {
		platformobservability.SysError("update security audit notification status failed: " + err.Error())
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}

func defaultText(value, fallback string) string {
	if strings.TrimSpace(value) == "" || value == "0" {
		return fallback
	}
	return value
}

func normalizeSeverity(value string) string {
	switch value = strings.ToLower(strings.TrimSpace(value)); value {
	case "low", "medium", "high", "critical":
		return value
	default:
		return "high"
	}
}
