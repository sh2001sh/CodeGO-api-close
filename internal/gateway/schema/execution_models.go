package schema

import (
	"strings"
	"time"

	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	RequestExecutionStatusRecorded         = "recorded"
	RequestExecutionStatusProviderComplete = "provider_completed"
	RequestExecutionStatusSettled          = "settled"
)

type RequestExecution struct {
	ExecutionID     string    `gorm:"column:execution_id;primaryKey;size:64"`
	RequestID       string    `gorm:"column:request_id;size:64;uniqueIndex:idx_request_executions_request_id"`
	TraceID         string    `gorm:"column:trace_id;size:64;index"`
	UserID          int       `gorm:"column:user_id;index"`
	TokenID         int       `gorm:"column:token_id;index"`
	AccountID       string    `gorm:"column:account_id;size:64;index"`
	ReservationID   string    `gorm:"column:reservation_id;size:64;index"`
	SettlementID    string    `gorm:"column:settlement_id;size:64;index"`
	RoutePlanID     string    `gorm:"column:route_plan_id;size:64;index"`
	Status          string    `gorm:"column:status;size:32;index"`
	ActualAmount    int64     `gorm:"column:actual_amount"`
	UsageEvidenceID string    `gorm:"column:usage_evidence_id;size:64;index"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt       time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (RequestExecution) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "gateway.request_executions"
	}
	return "gateway_request_executions"
}

func (record *RequestExecution) BeforeCreate(_ *gorm.DB) error {
	if strings.TrimSpace(record.ExecutionID) == "" {
		record.ExecutionID = platformruntime.GetUUID()
	}
	if strings.TrimSpace(record.Status) == "" {
		record.Status = RequestExecutionStatusRecorded
	}
	return nil
}

type GatewayRoutePlan struct {
	RoutePlanID string    `gorm:"column:route_plan_id;primaryKey;size:64"`
	RequestID   string    `gorm:"column:request_id;size:64;uniqueIndex:idx_route_plans_request_id"`
	TraceID     string    `gorm:"column:trace_id;size:64;index"`
	Status      string    `gorm:"column:status;size:32"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (GatewayRoutePlan) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "gateway.route_plans"
	}
	return "gateway_route_plans"
}

type ExecutionAttempt struct {
	AttemptID   string    `gorm:"column:attempt_id;primaryKey;size:64"`
	ExecutionID string    `gorm:"column:execution_id;size:64;index"`
	TraceID     string    `gorm:"column:trace_id;size:64;index"`
	AttemptNo   int       `gorm:"column:attempt_no"`
	Status      string    `gorm:"column:status;size:32"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (ExecutionAttempt) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "gateway.execution_attempts"
	}
	return "gateway_execution_attempts"
}

func (attempt *ExecutionAttempt) BeforeCreate(_ *gorm.DB) error {
	if strings.TrimSpace(attempt.AttemptID) == "" {
		attempt.AttemptID = platformruntime.GetUUID()
	}
	return nil
}

type UsageEvidence struct {
	UsageEvidenceID string    `gorm:"column:usage_evidence_id;primaryKey;size:64"`
	ExecutionID     string    `gorm:"column:execution_id;size:64;index"`
	RequestID       string    `gorm:"column:request_id;size:64;uniqueIndex:idx_usage_evidence_request_id"`
	TraceID         string    `gorm:"column:trace_id;size:64;index"`
	ActualAmount    int64     `gorm:"column:actual_amount"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (UsageEvidence) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "gateway.usage_evidence"
	}
	return "gateway_usage_evidence"
}

func (evidence *UsageEvidence) BeforeCreate(_ *gorm.DB) error {
	if strings.TrimSpace(evidence.UsageEvidenceID) == "" {
		evidence.UsageEvidenceID = platformruntime.GetUUID()
	}
	return nil
}

const (
	RequestAuditStatusInFlight  = "in_flight"
	RequestAuditStatusSucceeded = "succeeded"
	RequestAuditStatusFailed    = "failed"
	RequestAuditStatusRejected  = "rejected"
	RequestAuditStatusCancelled = "cancelled"
)

// RequestAudit is the canonical one-row-per-client-request record. It is
// intentionally separate from RequestExecution, which is the durable billing
// settlement projection and only exists for requests with a reservation.
type RequestAudit struct {
	RequestID            string    `gorm:"column:request_id;primaryKey;size:64"`
	TraceID              string    `gorm:"column:trace_id;size:64;index"`
	UserID               int       `gorm:"column:user_id;index"`
	TokenID              int       `gorm:"column:token_id;index"`
	ModelName            string    `gorm:"column:model_name;size:128;index"`
	Group                string    `gorm:"column:group_name;size:128;index"`
	Protocol             string    `gorm:"column:protocol;size:32"`
	RequestType          string    `gorm:"column:request_type;size:32"`
	Status               string    `gorm:"column:status;size:32;index"`
	CountedInSuccessRate bool      `gorm:"column:counted_in_success_rate;default:true"`
	Billable             bool      `gorm:"column:billable;default:false"`
	Quota                int64     `gorm:"column:quota;default:0"`
	PromptTokens         int64     `gorm:"column:prompt_tokens;default:0"`
	CompletionTokens     int64     `gorm:"column:completion_tokens;default:0"`
	FinalChannelID       int       `gorm:"column:final_channel_id;index"`
	AttemptsCount        int       `gorm:"column:attempts_count;default:0"`
	RetryCount           int       `gorm:"column:retry_count;default:0"`
	StatusCode           int       `gorm:"column:status_code;default:0"`
	ErrorCode            string    `gorm:"column:error_code;size:128"`
	StartedAt            time.Time `gorm:"column:started_at;index"`
	CompletedAt          time.Time `gorm:"column:completed_at;index"`
	CreatedAt            time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt            time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (RequestAudit) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "gateway.request_audits"
	}
	return "gateway_request_audits"
}

// RequestAttemptAudit stores one row for every upstream channel attempt,
// including attempts that were later hidden by a successful retry.
type RequestAttemptAudit struct {
	AttemptID    string    `gorm:"column:attempt_id;primaryKey;size:128"`
	RequestID    string    `gorm:"column:request_id;size:64;index:idx_request_attempt_audit_request"`
	AttemptNo    int       `gorm:"column:attempt_no"`
	RetryIndex   int       `gorm:"column:retry_index"`
	ChannelID    int       `gorm:"column:channel_id;index"`
	ModelName    string    `gorm:"column:model_name;size:128;index"`
	FaultDomain  string    `gorm:"column:fault_domain;size:128"`
	RequestType  string    `gorm:"column:request_type;size:32"`
	Status       string    `gorm:"column:status;size:32;index"`
	Success      bool      `gorm:"column:success;default:false"`
	StatusCode   int       `gorm:"column:status_code;default:0"`
	FailureClass string    `gorm:"column:failure_class;size:128"`
	Stage        string    `gorm:"column:stage;size:64"`
	StartedAt    time.Time `gorm:"column:started_at;index"`
	CompletedAt  time.Time `gorm:"column:completed_at;index"`
	DurationMS   int64     `gorm:"column:duration_ms;default:0"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (RequestAttemptAudit) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "gateway.request_attempt_audits"
	}
	return "gateway_request_attempt_audits"
}

func (attempt *RequestAttemptAudit) BeforeCreate(_ *gorm.DB) error {
	if strings.TrimSpace(attempt.AttemptID) == "" {
		attempt.AttemptID = platformruntime.GetUUID()
	}
	return nil
}
