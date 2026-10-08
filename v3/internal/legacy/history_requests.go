package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type historyRequestAudit struct {
	RequestID            string      `json:"request_id"`
	TraceID              string      `json:"trace_id"`
	UserID               int64       `json:"user_id"`
	KeyID                int64       `json:"token_id"`
	Model                string      `json:"model_name"`
	Group                string      `json:"group_name"`
	Protocol             string      `json:"protocol"`
	RequestType          string      `json:"request_type"`
	Status               string      `json:"status"`
	CountedInSuccessRate bool        `json:"counted_in_success_rate"`
	Billable             bool        `json:"billable"`
	Amount               int64       `json:"quota"`
	PromptTokens         int64       `json:"prompt_tokens"`
	CompletionTokens     int64       `json:"completion_tokens"`
	FinalChannelID       int64       `json:"final_channel_id"`
	AttemptsCount        int64       `json:"attempts_count"`
	RetryCount           int64       `json:"retry_count"`
	StatusCode           int64       `json:"status_code"`
	ErrorCode            string      `json:"error_code"`
	StartedAt            historyTime `json:"started_at"`
	CompletedAt          historyTime `json:"completed_at"`
	CreatedAt            historyTime `json:"created_at"`
	UpdatedAt            historyTime `json:"updated_at"`
}

func decodeHistoryRequestAudit(raw json.RawMessage) (historyRequestAudit, error) {
	var a historyRequestAudit
	if json.Unmarshal(raw, &a) != nil {
		return a, fmt.Errorf("invalid historical request audit")
	}
	if a.RequestID == "" {
		return a, fmt.Errorf("request audit ID is required")
	}
	if a.Amount < 0 || a.PromptTokens < 0 || a.CompletionTokens < 0 || a.AttemptsCount < 0 || a.RetryCount < 0 {
		return a, fmt.Errorf("request audit counters and amount must be nonnegative")
	}
	amount, err := FromV2Units(a.Amount)
	if err != nil {
		return a, fmt.Errorf("request audit amount overflows micro-credits")
	}
	a.Amount = int64(amount)
	if a.Status == "in_flight" {
		// Source drain validation separately proves the funding/provider work
		// terminal. It cannot recover the missing historical HTTP outcome. Keep
		// source amounts and times verbatim and exclude it from success scoring.
		a.Status = "historical_unknown"
		a.CountedInSuccessRate = false
	}
	return a, nil
}

func (a historyRequestAudit) completedDate() time.Time {
	if a.Status == "historical_unknown" {
		return a.CompletedAt.UTC()
	}
	return historyDate(a.CompletedAt)
}

type historyAttemptAudit struct {
	AttemptID    string      `json:"attempt_id"`
	RequestID    string      `json:"request_id"`
	AttemptNo    int64       `json:"attempt_no"`
	RetryIndex   int64       `json:"retry_index"`
	ChannelID    int64       `json:"channel_id"`
	Model        string      `json:"model_name"`
	FaultDomain  string      `json:"fault_domain"`
	RequestType  string      `json:"request_type"`
	Status       string      `json:"status"`
	Success      bool        `json:"success"`
	StatusCode   int64       `json:"status_code"`
	FailureClass string      `json:"failure_class"`
	Stage        string      `json:"stage"`
	StartedAt    historyTime `json:"started_at"`
	CompletedAt  historyTime `json:"completed_at"`
	DurationMS   int64       `json:"duration_ms"`
	CreatedAt    historyTime `json:"created_at"`
}

func decodeHistoryAttemptAudit(raw json.RawMessage) (historyAttemptAudit, error) {
	var a historyAttemptAudit
	if json.Unmarshal(raw, &a) != nil {
		return a, fmt.Errorf("invalid historical request attempt audit")
	}
	if a.AttemptID == "" || a.RequestID == "" || a.AttemptNo < 0 || a.RetryIndex < 0 || a.DurationMS < 0 {
		return a, fmt.Errorf("request attempt audit identifiers and nonnegative counters are required")
	}
	return a, nil
}

func (m *Importer) importHistoryRequestAudits(ctx context.Context, target pgx.Tx, d *historyData) error {
	requests := historyImportBatch(ctx, target, "v3_audit", "request_audits", "request_id")
	err := walkHistory(ctx, d.source, d.sources["request_audits"], func(raw json.RawMessage) error {
		a, err := decodeHistoryRequestAudit(raw)
		if err != nil {
			return err
		}
		columns := []string{"request_id", "trace_id", "user_id", "key_id", "model", "group_name", "protocol", "request_type", "status", "counted_in_success_rate", "billable", "amount", "prompt_tokens", "completion_tokens", "final_channel_id", "attempts_count", "retry_count", "status_code", "error_code", "started_at", "completed_at", "created_at", "updated_at"}
		values := []any{a.RequestID, a.TraceID, a.UserID, a.KeyID, a.Model, a.Group, a.Protocol, a.RequestType, a.Status, a.CountedInSuccessRate, a.Billable, a.Amount, a.PromptTokens, a.CompletionTokens, a.FinalChannelID, a.AttemptsCount, a.RetryCount, a.StatusCode, a.ErrorCode, historyDate(a.StartedAt), a.completedDate(), historyDate(a.CreatedAt), historyDate(a.UpdatedAt)}
		return requests.add(historyFields(columns, values))
	})
	if err != nil {
		return err
	}
	if err = requests.finish(); err != nil {
		return err
	}
	attempts := historyImportBatch(ctx, target, "v3_audit", "request_attempt_audits", "attempt_id")
	orphans := historyImportBatch(ctx, target, "v3_audit", "orphan_request_attempt_history", "attempt_id")
	err = walkHistoryAttempts(ctx, d.source, d.sources["request_attempt_audits"], d.sources["request_audits"], func(raw json.RawMessage, orphan bool) error {
		a, err := decodeHistoryAttemptAudit(raw)
		if err != nil {
			return err
		}
		if orphan {
			return orphans.add(map[string]any{"attempt_id": a.AttemptID, "request_id": a.RequestID, "source_record": raw})
		}
		columns := []string{"attempt_id", "request_id", "attempt_no", "retry_index", "channel_id", "model", "fault_domain", "request_type", "status", "success", "status_code", "failure_class", "stage", "started_at", "completed_at", "duration_ms", "created_at"}
		values := []any{a.AttemptID, a.RequestID, a.AttemptNo, a.RetryIndex, a.ChannelID, a.Model, a.FaultDomain, a.RequestType, a.Status, a.Success, a.StatusCode, a.FailureClass, a.Stage, historyDate(a.StartedAt), historyDate(a.CompletedAt), a.DurationMS, historyDate(a.CreatedAt)}
		return attempts.add(historyFields(columns, values))
	})
	if err != nil {
		return err
	}
	if err = attempts.finish(); err != nil {
		return err
	}
	return orphans.finish()
}
