package channelmarket

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type UsageLog struct {
	ID               int64     `json:"id"`
	RequestID        string    `json:"request_id"`
	CreatedAt        time.Time `json:"created_at"`
	ChannelID        int64     `json:"channel_id"`
	UserID           int64     `json:"user_id"`
	Amount           int64     `json:"amount_micro"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	Model            string    `json:"model"`
	Terminal         string    `json:"terminal"`
}

func (s *Service) Logs(ctx context.Context, a Actor, before time.Time, limit int) ([]UsageLog, error) {
	return s.LogsPage(ctx, a, before, 0, limit)
}

// LogsPage uses both ordering columns, so requests sharing a timestamp are
// retained on the next page. A zero ID preserves the older strict time cursor.
func (s *Service) LogsPage(ctx context.Context, a Actor, before time.Time, beforeID int64, limit int) ([]UsageLog, error) {
	return s.LogsPageFiltered(ctx, a, before, beforeID, limit, OwnerAnalyticsFilter{})
}

// LogsPageFiltered preserves the complete legacy report when no filters are
// provided; explicit dates use the same half-open bounds as owner analytics.
func (s *Service) LogsPageFiltered(ctx context.Context, a Actor, before time.Time, beforeID int64, limit int, f OwnerAnalyticsFilter) ([]UsageLog, error) {
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	if a.UserID <= 0 && !a.Admin || beforeID < 0 || before.IsZero() && beforeID != 0 {
		return nil, ErrInvalid
	}
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	if before.IsZero() {
		before = s.cfg.Now().Add(time.Minute)
	}
	if err := validateOwnerReportFilter(f); err != nil {
		return nil, err
	}
	if err := s.checkOwnerSelection(ctx, s.pool, a, f.ChannelID); err != nil {
		return nil, err
	}
	from, to := optionalOwnerBounds(f)
	rows, err := s.pool.Query(ctx, `SELECT l.id,l.request_id,l.created_at,l.channel_id,l.user_id,l.amount,l.prompt_tokens,l.completion_tokens,l.model,l.terminal
FROM v3_billing.usage_logs l JOIN v3_catalog.channels c ON c.id=l.channel_id LEFT JOIN v3_channelmarket.groups g ON g.channel_id=c.id
WHERE c.scope='marketplace' AND (c.owner_user_id=$1 OR $2) AND (l.created_at,l.id)<($3,$4)
AND ($6::timestamptz IS NULL OR l.created_at >= $6) AND ($7::timestamptz IS NULL OR l.created_at < $7)
AND ($8='' OR g.public_channel_id=$8) AND ($9='' OR l.model=$9)
ORDER BY l.created_at DESC,l.id DESC LIMIT $5`, a.UserID, a.Admin, before, beforeID, limit, from, to, f.ChannelID, f.Model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []UsageLog{}
	for rows.Next() {
		var l UsageLog
		if err = rows.Scan(&l.ID, &l.RequestID, &l.CreatedAt, &l.ChannelID, &l.UserID, &l.Amount, &l.PromptTokens, &l.CompletionTokens, &l.Model, &l.Terminal); err != nil {
			return nil, err
		}
		items = append(items, l)
	}
	return items, rows.Err()
}
func (s *Service) httpLogs(w http.ResponseWriter, r *http.Request, a Actor) {
	before, beforeID, err := logCursor(r)
	if err != nil {
		s.result(w, nil, err)
		return
	}
	f, err := ownerReportFilter(r)
	if err != nil {
		s.result(w, nil, err)
		return
	}
	result, err := s.LogsPageFiltered(r.Context(), a, before, beforeID, integer(r, "page_size", 100), f)
	s.result(w, result, err)
}

func logCursor(r *http.Request) (time.Time, int64, error) {
	var before time.Time
	var id int64
	var err error
	if v := r.URL.Query().Get("before"); v != "" {
		before, err = time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return before, id, ErrInvalid
		}
	}
	if v := r.URL.Query().Get("before_id"); v != "" {
		id, err = strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 || before.IsZero() {
			return before, id, ErrInvalid
		}
	}
	return before, id, nil
}
func csvText(v string) string {
	trimmed := strings.TrimLeft(v, " \t\r\n")
	if len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + v
	}
	return v
}
func (s *Service) httpExportLogs(w http.ResponseWriter, r *http.Request, a Actor) {
	f, err := ownerReportFilter(r)
	if err != nil {
		s.result(w, nil, err)
		return
	}
	s.exportCSV(w, r, "channel-usage.csv", func(writer *csv.Writer) error {
		return s.ExportLogsFiltered(r.Context(), a, f, writer)
	})
}
func (s *Service) httpUsage(w http.ResponseWriter, r *http.Request, a Actor) {
	f, err := ownerReportFilter(r)
	if err != nil {
		s.result(w, nil, err)
		return
	}
	items, err := s.UserUsageFiltered(r.Context(), a, f)
	s.result(w, items, err)
}
func (s *Service) httpTrends(w http.ResponseWriter, r *http.Request, _ Actor) {
	if s.pool == nil {
		s.result(w, nil, ErrUnavailable)
		return
	}
	rows, err := s.pool.Query(r.Context(), `SELECT t.group_id,t.multiplier_ppm,t.reliable,t.request_count,t.bucket_started_at FROM v3_channelmarket.multiplier_trend_snapshots t JOIN v3_channelmarket.groups g ON g.id=t.group_id WHERE g.visibility='public' AND g.lifecycle_status='active' AND g.deleted_at IS NULL ORDER BY t.bucket_started_at DESC LIMIT 1000`)
	if err != nil {
		s.result(w, nil, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id string
		var factor, count int64
		var reliable bool
		var bucket time.Time
		if err = rows.Scan(&id, &factor, &reliable, &count, &bucket); err != nil {
			s.result(w, nil, err)
			return
		}
		items = append(items, map[string]any{"group_id": id, "multiplier_ppm": factor, "reliable": reliable, "request_count": count, "bucket_started_at": bucket})
	}
	s.result(w, items, rows.Err())
}
func (s *Service) httpSecurity(w http.ResponseWriter, r *http.Request, a Actor) {
	if s.delegateSecurityAudit(w, r) {
		return
	}
	result, err := s.Security(r.Context(), a)
	s.result(w, result, err)
}
func (s *Service) httpResolveSecurity(w http.ResponseWriter, r *http.Request, a Actor) {
	if s.delegateSecurityAudit(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.result(w, nil, ErrInvalid)
		return
	}
	var input struct {
		Status  string          `json:"status"`
		Details json.RawMessage `json:"details,omitempty"`
	}
	if !decode(w, r, &input) {
		return
	}
	s.result(w, nil, s.ResolveSecurity(r.Context(), a, id, input.Status))
}
