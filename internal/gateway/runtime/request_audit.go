package runtime

import (
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm/clause"

	"github.com/sh2001sh/new-api/constant"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
)

// StartRequestAudit creates the canonical request-level row. It is best effort
// and asynchronous so the audit path does not add a database round trip to the
// API hot path. Finalization uses an upsert and remains correct if it wins the
// race with this initial insert.
func StartRequestAudit(c *gin.Context, model, group string) {
	if c == nil || platformdb.DB == nil {
		return
	}
	db := platformdb.DB
	requestID := c.GetString(constant.RequestIdKey)
	if requestID == "" {
		return
	}
	profile, _ := RequestProfileFromContext(c)
	start := time.Now().UTC()
	if value := httpctx.GetContextKeyTime(c, constant.ContextKeyRequestStartTime); !value.IsZero() {
		start = value.UTC()
	}
	record := gatewayschema.RequestAudit{
		RequestID:            requestID,
		TraceID:              c.GetString(constant.TraceIdKey),
		UserID:               c.GetInt("id"),
		TokenID:              c.GetInt("token_id"),
		ModelName:            model,
		Group:                group,
		Protocol:             profile.Protocol,
		RequestType:          string(profile.RequestType),
		Status:               gatewayschema.RequestAuditStatusInFlight,
		CountedInSuccessRate: true,
		StartedAt:            start,
	}
	gopool.Go(func() {
		err := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "request_id"}},
			DoNothing: true,
		}).Create(&record).Error
		if err != nil {
			platformobservability.SysError("failed to record request audit start: " + err.Error())
		}
	})
}

// FinalizeRequestAudit records exactly one terminal outcome per client request.
// It deliberately does not write a billing log; billing remains owned by the
// existing ledger and consume-log path.
func FinalizeRequestAudit(c *gin.Context, info *RelayInfo, apiErr *types.NewAPIError, upstreamStarted, countedInSuccessRate bool) {
	if c == nil || platformdb.DB == nil {
		return
	}
	db := platformdb.DB
	requestID := c.GetString(constant.RequestIdKey)
	if requestID == "" {
		return
	}
	model := c.GetString("original_model")
	group := c.GetString("group")
	userID := c.GetInt("id")
	tokenID := c.GetInt("token_id")
	protocol := ""
	requestType := string(RequestTypeFromContext(c))
	finalChannelID := c.GetInt("channel_id")
	quota := int64(0)
	billable := false
	if info != nil {
		model = info.OriginModelName
		group = info.UsingGroup
		userID = info.UserId
		tokenID = info.TokenId
		finalChannelID = info.ChannelId
		quota = int64(info.BillingSettledQuota)
		billable = info.BillingSettled
	}
	if decision, ok := GetRouteDecision(c); ok {
		protocol = decision.Protocol
		if finalChannelID <= 0 {
			finalChannelID = decision.ChannelID
		}
	}
	status := gatewayschema.RequestAuditStatusSucceeded
	if apiErr != nil {
		status = gatewayschema.RequestAuditStatusFailed
		if c.GetBool(string(constant.ContextKeyClientGone)) {
			status = gatewayschema.RequestAuditStatusCancelled
		}
		if !upstreamStarted {
			status = gatewayschema.RequestAuditStatusRejected
		}
	}
	attemptsCount, retryCount := 0, 0
	if decision, ok := GetRouteDecision(c); ok {
		attemptsCount = len(decision.Attempts)
		retryCount = decision.RetryCount
	}
	statusCode, errorCode := 0, ""
	if apiErr != nil {
		statusCode = apiErr.StatusCode
		errorCode = string(apiErr.GetErrorCode())
	}
	completedAt := time.Now().UTC()
	startedAt := completedAt
	if value := httpctx.GetContextKeyTime(c, constant.ContextKeyRequestStartTime); !value.IsZero() {
		startedAt = value.UTC()
	}
	record := gatewayschema.RequestAudit{
		RequestID:            requestID,
		TraceID:              c.GetString(constant.TraceIdKey),
		UserID:               userID,
		TokenID:              tokenID,
		ModelName:            model,
		Group:                group,
		Protocol:             protocol,
		RequestType:          requestType,
		Status:               status,
		CountedInSuccessRate: countedInSuccessRate,
		Billable:             billable,
		Quota:                quota,
		FinalChannelID:       finalChannelID,
		AttemptsCount:        attemptsCount,
		RetryCount:           retryCount,
		StatusCode:           statusCode,
		ErrorCode:            errorCode,
		StartedAt:            startedAt,
		CompletedAt:          completedAt,
		UpdatedAt:            completedAt,
	}
	gopool.Go(func() {
		err := db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "request_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"trace_id", "user_id", "token_id", "model_name", "group_name", "protocol", "request_type",
				"status", "counted_in_success_rate", "billable", "quota", "final_channel_id", "attempts_count",
				"retry_count", "status_code", "error_code", "started_at", "completed_at", "updated_at",
			}),
		}).Create(&record).Error
		if err != nil {
			platformobservability.SysError("failed to finalize request audit: " + err.Error())
		}
	})
}

func persistRequestAttempt(c *gin.Context, attempt RouteAttempt) {
	if c == nil || platformdb.DB == nil || attempt.AttemptID == "" || c.GetString(constant.RequestIdKey) == "" {
		return
	}
	db := platformdb.DB
	status := "failed"
	if attempt.Success {
		status = "succeeded"
	}
	record := gatewayschema.RequestAttemptAudit{
		AttemptID:    attempt.AttemptID,
		RequestID:    c.GetString(constant.RequestIdKey),
		AttemptNo:    len(mustRouteAttempts(c)),
		RetryIndex:   attempt.RetryIndex,
		ChannelID:    attempt.ChannelID,
		ModelName:    c.GetString("original_model"),
		FaultDomain:  attempt.FaultDomain,
		RequestType:  string(attempt.RequestType),
		Status:       status,
		Success:      attempt.Success,
		StatusCode:   attempt.StatusCode,
		FailureClass: attempt.FailureClass,
		Stage:        attempt.Stage,
		StartedAt:    attempt.StartedAt,
		CompletedAt:  time.Now().UTC(),
		DurationMS:   attempt.DurationMS,
	}
	gopool.Go(func() {
		err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "attempt_id"}}, DoNothing: true}).Create(&record).Error
		if err != nil {
			platformobservability.SysError("failed to record request attempt audit: " + err.Error())
		}
	})
}

func mustRouteAttempts(c *gin.Context) []RouteAttempt {
	if decision, ok := GetRouteDecision(c); ok {
		return decision.Attempts
	}
	return nil
}
