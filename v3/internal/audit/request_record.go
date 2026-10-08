package audit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// RequestRecord is a metadata-only terminal observation. Billing supplies the
// exact settled amount; this writer never computes prices or creates charges.
type RequestRecord struct {
	RequestID, Model, Group, Protocol, RequestType, Status, ErrorCode string
	UserID, KeyID, ChannelID, Attempts, Retries, StatusCode           int64
	PromptTokens, CompletionTokens                                    int64
	Counted, Billable                                                 bool
	Amount                                                            int64
	StartedAt, CompletedAt                                            time.Time
	TTFTMS, GenerationMS                                              *float64
}

// RecordRequest is idempotent by the logical request ID. Retries and repeated
// finalization do not add observations to the desktop success-rate denominator.
func (s *Service) RecordRequest(ctx context.Context, r RequestRecord) error {
	return s.RecordRequests(ctx, []RequestRecord{r})
}

// RecordRequests persists a bounded batch in one insert and one transaction.
// A conflicting request identity rolls back the whole batch, so callers never
// count partially committed records as written. Replays retain canonical facts.
func (s *Service) RecordRequests(ctx context.Context, records []RequestRecord) error {
	if len(records) == 0 {
		return nil
	}
	if len(records) > requestRecordBatchSize {
		return ErrInvalid
	}
	canonical := make([]RequestRecord, 0, len(records))
	byID := make(map[string]int, len(records))
	for _, r := range records {
		if !validRequestRecord(r) {
			return ErrInvalid
		}
		if index, exists := byID[r.RequestID]; exists {
			previous := &canonical[index]
			if previous.UserID != r.UserID || previous.KeyID != r.KeyID || previous.Model != r.Model || previous.Group != r.Group {
				return errors.New("audit: conflicting request identity")
			}
			previous.Amount = max(previous.Amount, r.Amount)
			if r.CompletedAt.After(previous.CompletedAt) {
				previous.CompletedAt = r.CompletedAt
			}
			if previous.TTFTMS == nil {
				previous.TTFTMS = r.TTFTMS
			}
			if previous.GenerationMS == nil {
				previous.GenerationMS = r.GenerationMS
			}
			continue
		}
		byID[r.RequestID] = len(canonical)
		canonical = append(canonical, r)
	}
	if s.pool == nil {
		return ErrUnavailable
	}
	var sql strings.Builder
	sql.WriteString(`INSERT INTO v3_audit.request_audits
 (request_id,trace_id,user_id,key_id,model,group_name,protocol,request_type,status,counted_in_success_rate,
 billable,amount,prompt_tokens,completion_tokens,final_channel_id,attempts_count,retry_count,status_code,error_code,
 started_at,completed_at,created_at,updated_at,ttft_ms,generation_ms) VALUES `)
	args := make([]any, 0, 22*len(canonical))
	for index, r := range canonical {
		if index > 0 {
			sql.WriteByte(',')
		}
		base := 22 * index
		fmt.Fprintf(&sql, "($%d,'',", base+1)
		for parameter := 2; parameter <= 20; parameter++ {
			if parameter > 2 {
				sql.WriteByte(',')
			}
			fmt.Fprintf(&sql, "$%d", base+parameter)
		}
		fmt.Fprintf(&sql, ",$%d,$%d,$%d,$%d)", base+19, base+20, base+21, base+22)
		args = append(args, r.RequestID, r.UserID, r.KeyID, r.Model, r.Group, r.Protocol, r.RequestType, r.Status,
			r.Counted, r.Billable, r.Amount, r.PromptTokens, r.CompletionTokens, r.ChannelID, r.Attempts, r.Retries,
			r.StatusCode, r.ErrorCode, r.StartedAt, r.CompletedAt, r.TTFTMS, r.GenerationMS)
	}
	sql.WriteString(` ON CONFLICT(request_id) DO UPDATE SET
 amount=greatest(v3_audit.request_audits.amount,excluded.amount),updated_at=greatest(v3_audit.request_audits.updated_at,excluded.updated_at),
 ttft_ms=COALESCE(v3_audit.request_audits.ttft_ms,excluded.ttft_ms),
 generation_ms=COALESCE(v3_audit.request_audits.generation_ms,excluded.generation_ms)
 WHERE v3_audit.request_audits.user_id=excluded.user_id AND v3_audit.request_audits.key_id=excluded.key_id
 AND v3_audit.request_audits.model=excluded.model AND v3_audit.request_audits.group_name=excluded.group_name`)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), requestRecordTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	tag, err := tx.Exec(ctx, sql.String(), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != int64(len(canonical)) {
		return errors.New("audit: conflicting request identity")
	}
	return tx.Commit(ctx)
}

func validRequestRecord(r RequestRecord) bool {
	if r.RequestID == "" || r.Model == "" || r.UserID <= 0 || r.KeyID <= 0 ||
		r.StartedAt.IsZero() || r.CompletedAt.Before(r.StartedAt) || r.PromptTokens < 0 ||
		r.CompletionTokens < 0 || r.Amount < 0 || r.Attempts < 0 || r.Retries < 0 ||
		!validPerformanceMS(r.TTFTMS) || !validPerformanceMS(r.GenerationMS) ||
		(r.TTFTMS != nil && r.RequestType != "stream") ||
		(r.GenerationMS != nil && (r.TTFTMS == nil || r.CompletionTokens <= 0 || r.Status != "success")) {
		return false
	}
	return true
}

func validPerformanceMS(value *float64) bool {
	return value == nil || (*value > 0 && !math.IsNaN(*value) && !math.IsInf(*value, 0))
}
