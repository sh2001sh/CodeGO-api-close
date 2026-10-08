package desktop

import (
	"net/http"
	"sort"
	"time"
)

func (s *Service) groupStatusHTTP(w http.ResponseWriter, r *http.Request) {
	d, ok := s.device(w, r, "account:read")
	if !ok {
		return
	}
	now := s.cfg.Now()
	end := now.Unix() - now.Unix()%1800 + 1800
	start := end - 12*1800
	stats, err := s.statusStats(r, d.UserID, start, end)
	if err != nil {
		reply(w, nil, err)
		return
	}
	rows, err := s.pool.Query(r.Context(), availableModels+`SELECT av.group_name,av.model,coalesce(m.id,p.id,'official:'||av.group_name),
	 coalesce(m.display_name,p.name,g.description,''),coalesce(m.source_type,CASE WHEN p.id IS NOT NULL THEN 'personal_pool' ELSE 'official' END)
	 FROM available av JOIN v3_catalog.groups g ON g.name=av.group_name
	 LEFT JOIN v3_channelmarket.groups m ON m.internal_group_name=av.group_name
	 LEFT JOIN v3_channelmarket.route_pools p ON p.internal_group_name=av.group_name
	 ORDER BY av.group_name,av.model`, d.UserID)
	if err != nil {
		reply(w, nil, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	indexes := map[string]int{}
	counts := map[string][2]float64{}
	for rows.Next() {
		var group, model, gid, display, source string
		if err := rows.Scan(&group, &model, &gid, &display, &source); err != nil {
			reply(w, nil, err)
			return
		}
		if display == "" {
			display = group
		}
		index, exists := indexes[group]
		if !exists {
			index = len(items)
			indexes[group] = index
			items = append(items, map[string]any{"group_id": gid, "group": group, "display_name": display, "source_type": source, "models": []map[string]any{}})
		}
		series := make([]statusBucket, 12)
		var latest *float64
		var count int64
		for i := range series {
			ts := start + int64(i)*1800
			v := stats.buckets[statusKey{group, model, ts}]
			series[i] = statusBucket{Ts: ts, RequestCount: v[0]}
			if v[0] > 0 {
				rate := float64(v[1]) / float64(v[0]) * 100
				series[i].SuccessRate = &rate
				latest = &rate
				count = v[0]
			}
		}
		item := map[string]any{"model": model, "status": healthStatus(latest, count), "success_rate": latest, "request_count": count, "cache_hit_rate": cacheRate(stats.cache[group+"::"+model]), "sample_window": 0.5, "series_window": 6, "bucket_seconds": 1800, "series": series}
		items[index]["models"] = append(items[index]["models"].([]map[string]any), item)
		v := counts[group]
		v[0] += float64(count)
		if latest != nil {
			v[1] += *latest * float64(count)
		}
		counts[group] = v
	}
	if err := rows.Err(); err != nil {
		reply(w, nil, err)
		return
	}
	for _, item := range items {
		group := item["group"].(string)
		v := counts[group]
		var rate *float64
		if v[0] > 0 {
			r := v[1] / v[0]
			rate = &r
		}
		item["status"], item["success_rate"], item["request_count"], item["cache_hit_rate"] = healthStatus(rate, int64(v[0])), rate, int64(v[0]), cacheRate(stats.cache[group])
		sort.SliceStable(item["models"].([]map[string]any), func(i, j int) bool {
			models := item["models"].([]map[string]any)
			return models[i]["request_count"].(int64) > models[j]["request_count"].(int64)
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i]["request_count"].(int64) > items[j]["request_count"].(int64) })
	reply(w, items, nil)
}

type statusBucket struct {
	Ts           int64    `json:"ts"`
	SuccessRate  *float64 `json:"success_rate"`
	RequestCount int64    `json:"request_count"`
}
type statusKey struct {
	group, model string
	ts           int64
}
type statusStats struct {
	buckets map[statusKey][2]int64
	cache   map[string][2]int64
}

func (s *Service) statusStats(r *http.Request, uid, start, end int64) (statusStats, error) {
	out := statusStats{map[statusKey][2]int64{}, map[string][2]int64{}}
	rows, err := s.pool.Query(r.Context(), availableModels+`SELECT a.group_name,a.model,(extract(epoch FROM a.started_at)::bigint/1800)*1800,count(*),count(*) FILTER(WHERE a.status='success')
	 FROM v3_audit.request_audits a WHERE a.started_at>=$2 AND a.started_at<$3 AND a.counted_in_success_rate
	 AND EXISTS(SELECT 1 FROM available av WHERE av.group_name=a.group_name AND av.model=a.model) GROUP BY 1,2,3`, uid, time.Unix(start, 0), time.Unix(end, 0))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var key statusKey
		var v [2]int64
		if err := rows.Scan(&key.group, &key.model, &key.ts, &v[0], &v[1]); err != nil {
			rows.Close()
			return out, err
		}
		out.buckets[key] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = s.pool.Query(r.Context(), availableModels+`SELECT a.group_name,l.model,sum(l.prompt_tokens)::bigint,sum(l.cached_tokens)::bigint FROM v3_billing.usage_logs l
	 JOIN v3_audit.request_audits a ON a.request_id=l.request_id WHERE l.created_at>=$2 AND NOT l.estimated
	 AND EXISTS(SELECT 1 FROM available av WHERE av.group_name=a.group_name AND av.model=l.model) GROUP BY 1,2`, uid, s.cfg.Now().Add(-24*time.Hour))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var g, m string
		var v [2]int64
		if err := rows.Scan(&g, &m, &v[0], &v[1]); err != nil {
			return out, err
		}
		out.cache[g+"::"+m] = v
		t := out.cache[g]
		t[0] += v[0]
		t[1] += v[1]
		out.cache[g] = t
	}
	return out, rows.Err()
}
func cacheRate(v [2]int64) *float64 {
	if v[0] <= 0 {
		return nil
	}
	rate := float64(v[1]) / float64(v[0]) * 100
	return &rate
}
func healthStatus(rate *float64, count int64) string {
	if rate == nil || count <= 0 {
		return "unknown"
	}
	if *rate > 90 {
		return "healthy"
	}
	if *rate >= 75 {
		return "unstable"
	}
	return "failed"
}
