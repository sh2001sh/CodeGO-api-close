package desktop

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/audit"
)

func (s *Service) summaryHTTP(w http.ResponseWriter, r *http.Request) {
	d, ok := s.device(w, r, "account:read")
	if !ok {
		return
	}
	ctx := r.Context()
	var username, display, group string
	var settings json.RawMessage
	var balance int64
	err := s.pool.QueryRow(ctx, `SELECT u.username,u.display_name,u.group_name,u.settings,coalesce(a.balance,0) FROM v3_identity.users u LEFT JOIN v3_billing.accounts a ON a.owner_type='user' AND a.owner_id=u.id AND a.kind='wallet' WHERE u.id=$1`, d.UserID).Scan(&username, &display, &group, &settings, &balance)
	if err != nil {
		reply(w, nil, err)
		return
	}
	var prefs struct {
		BillingPreference  string   `json:"billing_preference"`
		FundingSourceOrder []string `json:"funding_source_order"`
	}
	if err = json.Unmarshal(settings, &prefs); err != nil {
		reply(w, nil, err)
		return
	}
	all, err := s.audit.Summarize(ctx, audit.Principal{UserID: d.UserID}, audit.Query{})
	if err != nil {
		reply(w, nil, err)
		return
	}
	now := s.cfg.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	today, err := s.audit.Summarize(ctx, audit.Principal{UserID: d.UserID}, audit.Query{From: start})
	if err != nil {
		reply(w, nil, err)
		return
	}
	week, err := s.audit.Summarize(ctx, audit.Principal{UserID: d.UserID}, audit.Query{From: start.AddDate(0, 0, -6)})
	if err != nil {
		reply(w, nil, err)
		return
	}
	logs, err := s.audit.List(ctx, audit.Principal{UserID: d.UserID}, audit.Query{Limit: 10})
	if err != nil {
		reply(w, nil, err)
		return
	}
	var count int64
	err = s.pool.QueryRow(ctx, `SELECT count(*) FROM v3_identity.api_keys WHERE user_id=$1 AND deleted_at IS NULL`, d.UserID).Scan(&count)
	if err != nil {
		reply(w, nil, err)
		return
	}
	subs, err := s.subscriptionSnapshots(r, d.UserID)
	if err != nil {
		reply(w, nil, err)
		return
	}
	models, err := s.models(r, d.UserID)
	if err != nil {
		reply(w, nil, err)
		return
	}
	last := int64(0)
	if len(logs.Items) > 0 {
		last = logs.Items[0].CreatedAt.Unix()
	}
	service, err := s.serviceStatus(ctx)
	if err != nil {
		reply(w, nil, err)
		return
	}
	reply(w, map[string]any{
		"account":       map[string]any{"id": d.UserID, "username": username, "display_name": display, "group": group, "quota": balance / 2, "used_quota": int64(all.Amount) / 2, "request_count": all.Requests, "quota_usd": float64(balance) / 1e6, "used_quota_usd": float64(all.Amount) / 1e6, "billing_preference": prefs.BillingPreference, "funding_source_order": prefs.FundingSourceOrder, "balance_micro_credits": balance},
		"subscriptions": subs, "tokens": map[string]any{"total": count}, "usage": map[string]any{"available_models": models, "today_usd": float64(today.Amount) / 1e6, "last_7_days_usd": float64(week.Amount) / 1e6, "last_request_at": last},
		"service": service, "recent_logs": legacyLogs(logs.Items), "actions": map[string]any{"server_address": s.cfg.PublicURL, "topup_link": s.cfg.PublicURL + "/wallet", "tokens_path": "/keys", "logs_path": "/logs"},
	}, nil)
}

func (s *Service) subscriptionSnapshots(r *http.Request, uid int64) ([]map[string]any, error) {
	rows, err := s.pool.Query(r.Context(), `SELECT s.id,s.plan_id,p.name,s.total_credits,s.used_credits,s.period_credits,s.period_used,
	 extract(epoch FROM s.starts_at)::bigint,extract(epoch FROM s.expires_at)::bigint,coalesce(extract(epoch FROM s.next_reset_at)::bigint,0)
	 FROM v3_commerce.subscriptions s JOIN v3_commerce.plans p ON p.id=s.plan_id WHERE s.user_id=$1 AND s.state='active' AND s.deleted_at IS NULL AND s.expires_at>$2 ORDER BY s.id`, uid, s.cfg.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, pid, total, used, period, pused, start, end, reset int64
		var name string
		if err := rows.Scan(&id, &pid, &name, &total, &used, &period, &pused, &start, &end, &reset); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "plan_id": pid, "plan_title": name, "amount_total_usd": float64(total) / 1e6, "amount_used_usd": float64(used) / 1e6, "remaining_usd": float64(max(0, total-used)) / 1e6, "unlimited": total <= 0, "period_amount_usd": float64(period) / 1e6, "period_used_usd": float64(pused) / 1e6, "period_remaining_usd": float64(max(0, period-pused)) / 1e6, "start_time": start, "end_time": end, "next_reset_time": reset})
	}
	return out, rows.Err()
}

func (s *Service) models(r *http.Request, uid int64) ([]string, error) {
	rows, err := s.pool.Query(r.Context(), availableModels+`SELECT DISTINCT model FROM available ORDER BY model`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (s *Service) groupsHTTP(w http.ResponseWriter, r *http.Request) {
	d, ok := s.device(w, r, "account:read")
	if !ok {
		return
	}
	var group string
	if err := s.pool.QueryRow(r.Context(), `SELECT group_name FROM v3_identity.users WHERE id=$1`, d.UserID).Scan(&group); err != nil {
		reply(w, nil, err)
		return
	}
	rows, err := s.pool.Query(r.Context(), `SELECT g.name,g.description,g.multiplier::double precision,(SELECT count(DISTINCT cm.model) FROM v3_catalog.channel_models cm JOIN v3_catalog.channels c ON c.id=cm.channel_id JOIN v3_catalog.channel_groups cg ON cg.channel_id=c.id WHERE c.status='enabled' AND cg.group_name=g.name) FROM v3_catalog.groups g WHERE g.name IN (SELECT v3_identity.allowed_groups($1)) ORDER BY g.name`, d.UserID)
	if err != nil {
		reply(w, nil, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var name, desc string
		var ratio float64
		var count int64
		if err := rows.Scan(&name, &desc, &ratio, &count); err != nil {
			reply(w, nil, err)
			return
		}
		items = append(items, map[string]any{"name": name, "desc": desc, "ratio": ratio, "current": name == group, "available_models_count": count})
	}
	reply(w, map[string]any{"current": group, "items": items}, rows.Err())
}
