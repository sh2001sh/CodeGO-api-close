package desktop

import (
	"net/http"
	"strconv"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/legacy/identitydto"
)

func legacyLogs(items []audit.Usage) []map[string]any {
	out := []map[string]any{}
	for _, u := range items {
		out = append(out, map[string]any{"id": u.ID, "user_id": u.UserID, "created_at": u.CreatedAt.Unix(), "type": 2, "token_id": u.KeyID, "channel_id": u.ChannelID, "model_name": u.Model, "quota": int64(u.Amount) / 2, "amount_micro_credits": u.Amount, "prompt_tokens": u.PromptTokens, "completion_tokens": u.CompletionTokens, "request_id": u.RequestID, "content": u.Terminal, "other": "{}"})
	}
	return out
}
func usageQuery(r *http.Request) (audit.Query, error) {
	v := r.URL.Query()
	q := audit.Query{Cursor: v.Get("cursor"), Model: v.Get("model_name")}
	if q.Model == "" {
		q.Model = v.Get("model")
	}
	limit := v.Get("page_size")
	if limit == "" {
		limit = v.Get("ps")
	}
	if limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n < 1 || n > 200 {
			return q, ErrInvalid
		}
		q.Limit = n
	}
	for key, dst := range map[string]*time.Time{"start_timestamp": &q.From, "end_timestamp": &q.To} {
		if raw := v.Get(key); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || n < 0 || n > 253402300799 {
				return q, ErrInvalid
			}
			*dst = time.Unix(n, 0)
		}
	}
	return q, nil
}
func (s *Service) logsHTTP(w http.ResponseWriter, r *http.Request) {
	d, ok := s.device(w, r, "logs:read")
	if !ok {
		return
	}
	q, err := usageQuery(r)
	if err != nil {
		reply(w, nil, err)
		return
	}
	if identitydto.IsV3(r) {
		out, err := s.audit.List(r.Context(), audit.Principal{UserID: d.UserID}, q)
		reply(w, out, err)
		return
	}
	page := int(positiveID(r.URL.Query().Get("p")))
	if page == 0 {
		page = int(positiveID(r.URL.Query().Get("page")))
	}
	if page < 1 {
		page = 1
	}
	if page > 10000 {
		reply(w, nil, ErrInvalid)
		return
	}
	if q.Limit == 0 {
		q.Limit = 10
	}
	items, total, err := s.legacyUsagePage(r, d.UserID, q, page)
	reply(w, map[string]any{"page": page, "page_size": q.Limit, "total": total, "items": items}, err)
}

func (s *Service) trendsHTTP(w http.ResponseWriter, r *http.Request) {
	d, ok := s.device(w, r, "logs:read")
	if !ok {
		return
	}
	days := 7
	if raw := r.URL.Query().Get("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 30 {
			reply(w, nil, ErrInvalid)
			return
		}
		days = n
	}
	now := s.cfg.Now()
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1)
	start := end.AddDate(0, 0, -days)
	rows, err := s.pool.Query(r.Context(), `SELECT extract(epoch FROM date_trunc('day',created_at AT TIME ZONE $4) AT TIME ZONE $4)::bigint,count(*),coalesce(sum(amount),0)::bigint,coalesce(sum(prompt_tokens+completion_tokens),0)::bigint FROM v3_billing.usage_logs WHERE user_id=$1 AND created_at>=$2 AND created_at<$3 GROUP BY 1`, d.UserID, start, end, now.Format("-07:00"))
	if err != nil {
		reply(w, nil, err)
		return
	}
	defer rows.Close()
	stats := map[int64][3]int64{}
	for rows.Next() {
		var stamp, requests, amount, tokens int64
		if err := rows.Scan(&stamp, &requests, &amount, &tokens); err != nil {
			reply(w, nil, err)
			return
		}
		stats[stamp] = [3]int64{requests, amount, tokens}
	}
	trend := []map[string]any{}
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i)
		v := stats[day.Unix()]
		trend = append(trend, map[string]any{"date": day.Format("2006-01-02"), "timestamp": day.Unix(), "requests": v[0], "quota": v[1] / 2, "token_used": v[2], "quota_usd": float64(v[1]) / 1e6})
	}
	reply(w, map[string]any{"days": days, "trend": trend}, rows.Err())
}
