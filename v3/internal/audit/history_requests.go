package audit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

type requestCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func encodeRequestCursor(c requestCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeRequestCursor(value string) (requestCursor, error) {
	var c requestCursor
	if value == "" {
		return c, nil
	}
	if len(value) > 2048 {
		return c, ErrInvalid
	}
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || json.Unmarshal(b, &c) != nil || c.At.IsZero() || c.ID == "" || len(c.ID) > 1024 {
		return requestCursor{}, ErrInvalid
	}
	return c, nil
}

const requestWhere = `WHERE ($1::bigint=0 OR user_id=$1)
 AND ($2::bigint=0 OR key_id=$2) AND ($3::bigint=0 OR final_channel_id=$3)
 AND ($4::text='' OR model=$4)
 AND ($5::timestamptz IS NULL OR started_at>=$5)
 AND ($6::timestamptz IS NULL OR started_at<$6)`

func (s *Service) ListRequests(ctx context.Context, p Principal, query Query) (RequestPage, error) {
	c, err := decodeRequestCursor(query.Cursor)
	if err != nil {
		return RequestPage{}, err
	}
	query.Cursor = ""
	q, err := scope(p, query)
	if err != nil {
		return RequestPage{}, err
	}
	if s.pool == nil {
		return RequestPage{}, ErrUnavailable
	}
	args := append(queryArgs(q), timeArg(c.At), c.ID, q.Limit+1)
	rows, err := s.pool.Query(ctx, `SELECT request_id,trace_id,user_id,key_id,model,group_name,protocol,
 request_type,status,counted_in_success_rate,billable,amount,prompt_tokens,completion_tokens,
 final_channel_id,attempts_count,retry_count,status_code,error_code,started_at,completed_at
 FROM v3_audit.request_audits `+requestWhere+`
 AND ($7::timestamptz IS NULL OR (started_at,request_id)<($7,$8))
 ORDER BY started_at DESC,request_id DESC LIMIT $9`, args...)
	if err != nil {
		return RequestPage{}, err
	}
	defer rows.Close()
	page := RequestPage{Items: make([]RequestAudit, 0, q.Limit), PageSize: q.Limit}
	for rows.Next() {
		var a RequestAudit
		if err := rows.Scan(&a.RequestID, &a.TraceID, &a.UserID, &a.KeyID, &a.Model, &a.GroupName,
			&a.Protocol, &a.RequestType, &a.Status, &a.CountedInSuccessRate, &a.Billable, &a.Amount,
			&a.PromptTokens, &a.CompletionTokens, &a.FinalChannelID, &a.AttemptsCount, &a.RetryCount,
			&a.StatusCode, &a.ErrorCode, &a.StartedAt, &a.CompletedAt); err != nil {
			return RequestPage{}, err
		}
		page.Items = append(page.Items, a)
	}
	if err := rows.Err(); err != nil {
		return RequestPage{}, err
	}
	if len(page.Items) > q.Limit {
		page.Items = page.Items[:q.Limit]
		page.HasMore = true
		a := page.Items[len(page.Items)-1]
		page.NextCursor = encodeRequestCursor(requestCursor{At: a.StartedAt, ID: a.RequestID})
	}
	return page, nil
}

type attemptCursor struct {
	Number int64  `json:"number"`
	ID     string `json:"id"`
}

func encodeAttemptCursor(c attemptCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeAttemptCursor(value string) (attemptCursor, error) {
	var c attemptCursor
	if value == "" {
		return c, nil
	}
	if len(value) > 2048 {
		return c, ErrInvalid
	}
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || json.Unmarshal(b, &c) != nil || c.Number < 0 || c.ID == "" || len(c.ID) > 1024 {
		return attemptCursor{}, ErrInvalid
	}
	return c, nil
}

func (s *Service) ListAttempts(ctx context.Context, p Principal, requestID, after string, limit int) (AttemptPage, error) {
	if strings.TrimSpace(requestID) == "" || len(requestID) > 1024 {
		return AttemptPage{}, ErrInvalid
	}
	c, err := decodeAttemptCursor(after)
	if err != nil {
		return AttemptPage{}, err
	}
	q, err := scope(p, Query{Limit: limit})
	if err != nil {
		return AttemptPage{}, err
	}
	if s.pool == nil {
		return AttemptPage{}, ErrUnavailable
	}
	var visible bool
	err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_audit.request_audits
 WHERE request_id=$1 AND ($2::bigint=0 OR user_id=$2) AND ($3::bigint=0 OR key_id=$3))`,
		requestID, q.UserID, q.KeyID).Scan(&visible)
	if err != nil {
		return AttemptPage{}, err
	}
	if !visible {
		return AttemptPage{}, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `SELECT a.attempt_id,a.request_id,a.attempt_no,a.retry_index,
 a.channel_id,a.model,a.fault_domain,a.request_type,a.status,a.success,a.status_code,
 a.failure_class,a.stage,a.started_at,a.completed_at,a.duration_ms
 FROM v3_audit.request_attempt_audits a JOIN v3_audit.request_audits r USING(request_id)
 WHERE a.request_id=$1 AND ($2::bigint=0 OR r.user_id=$2) AND ($3::bigint=0 OR r.key_id=$3)
 AND ($4::text='' OR (a.attempt_no,a.attempt_id)>($5,$4))
 ORDER BY a.attempt_no,a.attempt_id LIMIT $6`, requestID, q.UserID, q.KeyID, c.ID, c.Number, q.Limit+1)
	if err != nil {
		return AttemptPage{}, err
	}
	defer rows.Close()
	page := AttemptPage{Items: make([]AttemptAudit, 0, q.Limit), PageSize: q.Limit}
	for rows.Next() {
		var a AttemptAudit
		if err := rows.Scan(&a.AttemptID, &a.RequestID, &a.AttemptNo, &a.RetryIndex, &a.ChannelID,
			&a.Model, &a.FaultDomain, &a.RequestType, &a.Status, &a.Success, &a.StatusCode,
			&a.FailureClass, &a.Stage, &a.StartedAt, &a.CompletedAt, &a.DurationMS); err != nil {
			return AttemptPage{}, err
		}
		page.Items = append(page.Items, a)
	}
	if err := rows.Err(); err != nil {
		return AttemptPage{}, err
	}
	if len(page.Items) > q.Limit {
		page.Items = page.Items[:q.Limit]
		page.HasMore = true
		a := page.Items[len(page.Items)-1]
		page.NextCursor = encodeAttemptCursor(attemptCursor{Number: a.AttemptNo, ID: a.AttemptID})
	}
	return page, nil
}
