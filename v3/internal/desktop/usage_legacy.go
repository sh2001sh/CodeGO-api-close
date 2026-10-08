package desktop

import (
	"net/http"
	"strconv"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/audit"
)

const logRows = `WITH logs AS (
 SELECT e.id,e.user_id,e.created_at,e.event_type,e.content,e.token_name,e.model,e.amount,e.prompt_tokens,e.completion_tokens,
 e.duration_seconds,e.is_stream,e.channel_id,e.key_id,e.group_name,e.request_id,e.upstream_request_id,e.metadata::text AS other
 FROM v3_audit.events e WHERE e.user_id=$1
 UNION ALL
 SELECT l.id,l.user_id,l.created_at,2,l.terminal,coalesce(k.name,''),l.model,l.amount,l.prompt_tokens,l.completion_tokens,
 0,false,l.channel_id,l.key_id,coalesce(a.group_name,k.group_name,u.group_name,''),l.request_id,''::text,'{}'::text
 FROM v3_billing.usage_logs l LEFT JOIN v3_identity.api_keys k ON k.id=l.key_id LEFT JOIN v3_identity.users u ON u.id=l.user_id
 LEFT JOIN v3_audit.request_audits a ON a.request_id=l.request_id
 WHERE l.user_id=$1 AND NOT EXISTS(SELECT 1 FROM v3_audit.events e WHERE e.user_id=l.user_id AND e.event_type=2 AND e.request_id<>'' AND e.request_id=l.request_id)
) `

func (s *Service) legacyUsagePage(r *http.Request, uid int64, q audit.Query, page int) ([]map[string]any, int64, error) {
	v := r.URL.Query()
	eventType := 0
	if raw := v.Get("type"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 6 {
			return nil, 0, ErrInvalid
		}
		eventType = n
	}
	from, to := any(nil), any(nil)
	if !q.From.IsZero() {
		from = q.From
	}
	if !q.To.IsZero() {
		to = q.To
	}
	if from != nil && to != nil && !q.From.Before(q.To) {
		return nil, 0, ErrInvalid
	}
	where := ` WHERE ($2::text='' OR model=$2) AND ($3::timestamptz IS NULL OR created_at>=$3) AND ($4::timestamptz IS NULL OR created_at<$4)
	 AND ($5::text='' OR request_id=$5) AND ($6::int=0 OR event_type=$6) AND ($7::text='' OR token_name=$7)
	 AND ($8::text='' OR group_name=$8) AND ($9::text='' OR upstream_request_id=$9)`
	args := []any{uid, q.Model, from, to, v.Get("request_id"), eventType, v.Get("token_name"), v.Get("group"), v.Get("upstream_request_id")}
	var total int64
	if err := s.pool.QueryRow(r.Context(), logRows+`SELECT count(*) FROM logs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(r.Context(), logRows+`SELECT id,user_id,created_at,event_type,content,token_name,model,amount,prompt_tokens,completion_tokens,duration_seconds,is_stream,channel_id,key_id,group_name,request_id,upstream_request_id,other FROM logs`+where+` ORDER BY created_at DESC,id DESC LIMIT $10 OFFSET $11`, append(args, q.Limit, (page-1)*q.Limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, user, amount, prompt, completion, duration, channel, key int64
		var at time.Time
		var typ int
		var stream bool
		var content, token, model, group, request, upstream, other string
		if err := rows.Scan(&id, &user, &at, &typ, &content, &token, &model, &amount, &prompt, &completion, &duration, &stream, &channel, &key, &group, &request, &upstream, &other); err != nil {
			return nil, 0, err
		}
		out = append(out, map[string]any{"id": id, "user_id": user, "created_at": at.Unix(), "type": typ, "content": content, "token_name": token, "model_name": model, "quota": amount / 2, "amount_micro_credits": amount, "prompt_tokens": prompt, "completion_tokens": completion, "use_time": duration, "is_stream": stream, "channel_id": channel, "token_id": key, "group": group, "request_id": request, "upstream_request_id": upstream, "other": other})
	}
	return out, total, rows.Err()
}
