package audit

import (
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Event exposes the original log semantics. Storage-only IP, token name and
// arbitrary metadata are excluded from the response.
type Event struct {
	ID               int64         `json:"id"`
	UserID           int64         `json:"user_id"`
	KeyID            int64         `json:"key_id"`
	CreatedAt        time.Time     `json:"created_at"`
	EventType        int           `json:"event_type"`
	Content          string        `json:"content"`
	Model            string        `json:"model"`
	Amount           credits.Micro `json:"amount,string"`
	PromptTokens     int64         `json:"prompt_tokens"`
	CompletionTokens int64         `json:"completion_tokens"`
	DurationSeconds  int64         `json:"duration_seconds"`
	IsStream         bool          `json:"is_stream"`
	ChannelID        int64         `json:"channel_id"`
	GroupName        string        `json:"group_name"`
	RequestID        string        `json:"request_id"`
}

type EventQuery struct {
	Query
	EventType *int
}

type EventPage struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"next_cursor,omitempty"`
	HasMore    bool    `json:"has_more"`
	PageSize   int     `json:"page_size"`
}

type RequestAudit struct {
	RequestID            string        `json:"request_id"`
	TraceID              string        `json:"trace_id"`
	UserID               int64         `json:"user_id"`
	KeyID                int64         `json:"key_id"`
	Model                string        `json:"model"`
	GroupName            string        `json:"group_name"`
	Protocol             string        `json:"protocol"`
	RequestType          string        `json:"request_type"`
	Status               string        `json:"status"`
	CountedInSuccessRate bool          `json:"counted_in_success_rate"`
	Billable             bool          `json:"billable"`
	Amount               credits.Micro `json:"amount,string"`
	PromptTokens         int64         `json:"prompt_tokens"`
	CompletionTokens     int64         `json:"completion_tokens"`
	FinalChannelID       int64         `json:"final_channel_id"`
	AttemptsCount        int64         `json:"attempts_count"`
	RetryCount           int64         `json:"retry_count"`
	StatusCode           int64         `json:"status_code"`
	ErrorCode            string        `json:"error_code"`
	StartedAt            time.Time     `json:"started_at"`
	CompletedAt          time.Time     `json:"completed_at"`
}

type RequestPage struct {
	Items      []RequestAudit `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
	HasMore    bool           `json:"has_more"`
	PageSize   int            `json:"page_size"`
}

type AttemptAudit struct {
	AttemptID    string    `json:"attempt_id"`
	RequestID    string    `json:"request_id"`
	AttemptNo    int64     `json:"attempt_no"`
	RetryIndex   int64     `json:"retry_index"`
	ChannelID    int64     `json:"channel_id"`
	Model        string    `json:"model"`
	FaultDomain  string    `json:"fault_domain"`
	RequestType  string    `json:"request_type"`
	Status       string    `json:"status"`
	Success      bool      `json:"success"`
	StatusCode   int64     `json:"status_code"`
	FailureClass string    `json:"failure_class"`
	Stage        string    `json:"stage"`
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at"`
	DurationMS   int64     `json:"duration_ms"`
}

type AttemptPage struct {
	Items      []AttemptAudit `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
	HasMore    bool           `json:"has_more"`
	PageSize   int            `json:"page_size"`
}
