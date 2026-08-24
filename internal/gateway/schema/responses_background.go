package schema

import (
	"time"

	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

const (
	ResponsesBackgroundQueued    = "queued"
	ResponsesBackgroundRunning   = "in_progress"
	ResponsesBackgroundCompleted = "completed"
	ResponsesBackgroundFailed    = "failed"
	ResponsesBackgroundCanceled  = "cancelled"
)

type ResponsesBackgroundJob struct {
	ID                       string     `json:"id" gorm:"column:id;primaryKey;size:64"`
	UserID                   int        `json:"-" gorm:"column:user_id;not null;index:idx_responses_background_owner,priority:1"`
	TokenID                  int        `json:"-" gorm:"column:token_id;not null;index:idx_responses_background_owner,priority:2"`
	Model                    string     `json:"model" gorm:"column:model;size:255;not null"`
	Status                   string     `json:"status" gorm:"column:status;size:24;not null;index"`
	Stream                   bool       `json:"-" gorm:"column:stream;not null;default:false"`
	NativeBackground         bool       `json:"-" gorm:"column:native_background;not null;default:false"`
	ChannelID                int        `json:"-" gorm:"column:channel_id;not null;index"`
	KeyIndex                 int        `json:"-" gorm:"column:key_index;not null;default:0"`
	RequestCiphertext        string     `json:"-" gorm:"column:request_ciphertext;type:text;not null"`
	AuthorizationCiphertext  string     `json:"-" gorm:"column:authorization_ciphertext;type:text;not null"`
	ClientIPCiphertext       string     `json:"-" gorm:"column:client_ip_ciphertext;type:text"`
	RoutingContextCiphertext string     `json:"-" gorm:"column:routing_context_ciphertext;type:text;not null"`
	FinalResponseCiphertext  string     `json:"-" gorm:"column:final_response_ciphertext;type:text"`
	ErrorCiphertext          string     `json:"-" gorm:"column:error_ciphertext;type:text"`
	UpstreamResponseID       string     `json:"-" gorm:"column:upstream_response_id;size:128;index"`
	UpstreamSequence         int64      `json:"-" gorm:"column:upstream_sequence;not null;default:-1"`
	LastSequence             int64      `json:"-" gorm:"column:last_sequence;not null;default:-1"`
	CancelRequested          bool       `json:"-" gorm:"column:cancel_requested;not null;default:false;index"`
	ClaimedAt                *time.Time `json:"-" gorm:"column:claimed_at;index"`
	StartedAt                *time.Time `json:"-" gorm:"column:started_at"`
	CompletedAt              *time.Time `json:"-" gorm:"column:completed_at;index"`
	CreatedAt                time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime;index"`
	UpdatedAt                time.Time  `json:"-" gorm:"column:updated_at;autoCreateTime;autoUpdateTime"`
}

func (ResponsesBackgroundJob) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "gateway.responses_background_jobs"
	}
	return "gateway_responses_background_jobs"
}

type ResponsesBackgroundEvent struct {
	ID                uint64    `json:"-" gorm:"primaryKey;autoIncrement"`
	JobID             string    `json:"-" gorm:"column:job_id;size:64;not null;uniqueIndex:uq_responses_background_event,priority:1;index"`
	Sequence          int64     `json:"sequence_number" gorm:"column:sequence;not null;uniqueIndex:uq_responses_background_event,priority:2"`
	Type              string    `json:"type" gorm:"column:type;size:64;not null"`
	PayloadCiphertext string    `json:"-" gorm:"column:payload_ciphertext;type:text;not null"`
	CreatedAt         time.Time `json:"-" gorm:"column:created_at;autoCreateTime"`
}

func (ResponsesBackgroundEvent) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "gateway.responses_background_events"
	}
	return "gateway_responses_background_events"
}

func IsResponsesBackgroundTerminal(status string) bool {
	switch status {
	case ResponsesBackgroundCompleted, ResponsesBackgroundFailed, ResponsesBackgroundCanceled:
		return true
	default:
		return false
	}
}
