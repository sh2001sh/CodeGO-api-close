package channelmarket

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func wilson(success, total int64) float64 {
	if total == 0 {
		return 0
	}
	n := float64(total)
	p := float64(success) / n
	const z2 = 3.8416
	return (p + z2/(2*n) - 1.96*math.Sqrt((p*(1-p)+z2/(4*n))/n)) / (1 + z2/n)
}

// rankingMetric is one group's aggregated 24h usage metrics and Wilson score.
type rankingMetric struct {
	id, label                                   string
	channel, factor, total, success, users, avg int64
	cache                                       float64
	models                                      []string
	score                                       float64
}

// RefreshRankings uses persisted usage only. Unmeasured latency/throughput stays
// absent rather than manufacturing samples for an empty channel.
func (s *Service) RefreshRankings(ctx context.Context) (int, error) {
	count := 0
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		metrics, e := loadRankingMetricsTx(ctx, tx, s.cfg.Now())
		if e != nil {
			return e
		}
		sort.Slice(metrics, func(i, j int) bool {
			if metrics[i].score == metrics[j].score {
				return metrics[i].id < metrics[j].id
			}
			return metrics[i].score > metrics[j].score
		})
		bucket := s.cfg.Now().Truncate(30 * time.Minute)
		for rank, m := range metrics {
			if e := saveRankingSnapshotTx(ctx, tx, s.cfg.Now(), bucket, rank, m); e != nil {
				return e
			}
			count++
		}
		return nil
	})
	return count, err
}

// loadRankingMetricsTx aggregates each market group's usage over the last
// 24 hours and computes its Wilson success score.
func loadRankingMetricsTx(ctx context.Context, tx pgx.Tx, now time.Time) ([]rankingMetric, error) {
	rows, e := tx.Query(ctx, `SELECT g.id,g.channel_id,g.source_label,g.multiplier_ppm,
	count(l.id),count(l.id) FILTER(WHERE l.terminal IN ('completed','Completed','completed_no_usage','CompletedNoUsage')),
	count(DISTINCT l.user_id),coalesce(round(avg(l.amount)),0)::bigint,
	coalesce(sum(l.cached_tokens)::numeric/nullif(sum(l.prompt_tokens),0),0)::float8,
	ARRAY(SELECT model FROM v3_catalog.channel_models WHERE channel_id=g.channel_id ORDER BY model)
	FROM v3_channelmarket.groups g LEFT JOIN v3_billing.usage_logs l ON l.channel_id=g.channel_id AND l.created_at>$1
	WHERE g.deleted_at IS NULL GROUP BY g.id ORDER BY g.id`, now.Add(-24*time.Hour))
	if e != nil {
		return nil, e
	}
	var metrics []rankingMetric
	for rows.Next() {
		var m rankingMetric
		if e = rows.Scan(&m.id, &m.channel, &m.label, &m.factor, &m.total, &m.success, &m.users, &m.avg, &m.cache, &m.models); e != nil {
			rows.Close()
			return nil, e
		}
		m.score = wilson(m.success, m.total)
		metrics = append(metrics, m)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	return metrics, nil
}

// saveRankingSnapshotTx persists one group's ranking and multiplier-trend
// snapshot rows for the current calculation pass.
func saveRankingSnapshotTx(ctx context.Context, tx pgx.Tx, now, bucket time.Time, rank int, m rankingMetric) error {
	raw := float64(0)
	if m.total > 0 {
		raw = float64(m.success) / float64(m.total)
	}
	if _, e := tx.Exec(ctx, `INSERT INTO v3_channelmarket.ranking_snapshots(id,group_id,window_hours,ranking_version,rank,score,raw_success_rate,wilson_success_rate,cache_hit_rate,avg_consumer_micro,request_count,independent_consumers,observing,calculated_at) VALUES($1,$2,24,'v3-usage',$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(group_id,window_hours,ranking_version) DO UPDATE SET rank=EXCLUDED.rank,score=EXCLUDED.score,raw_success_rate=EXCLUDED.raw_success_rate,wilson_success_rate=EXCLUDED.wilson_success_rate,cache_hit_rate=EXCLUDED.cache_hit_rate,avg_consumer_micro=EXCLUDED.avg_consumer_micro,request_count=EXCLUDED.request_count,independent_consumers=EXCLUDED.independent_consumers,observing=EXCLUDED.observing,calculated_at=EXCLUDED.calculated_at`, "v3-usage:"+m.id, m.id, rank+1, m.score*100, raw, m.score, m.cache, m.avg, m.total, m.users, m.total < 10, now); e != nil {
		return e
	}
	models, e := json.Marshal(m.models)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO v3_channelmarket.multiplier_trend_snapshots(group_id,channel_id,source_label,models,multiplier_ppm,reliable,request_count,wilson_success_rate,bucket_started_at,captured_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(group_id,bucket_started_at) DO UPDATE SET multiplier_ppm=EXCLUDED.multiplier_ppm,reliable=EXCLUDED.reliable,request_count=EXCLUDED.request_count,wilson_success_rate=EXCLUDED.wilson_success_rate,captured_at=EXCLUDED.captured_at`, m.id, m.channel, m.label, models, m.factor, m.total >= 10, m.total, m.score, bucket, now)
	return e
}

type autoBuild struct {
	Enabled        bool       `json:"enabled"`
	Model          string     `json:"model"`
	Models         []string   `json:"models"`
	Size           int        `json:"size"`
	Interval       int        `json:"interval_minutes"`
	Next           *time.Time `json:"next_build_at"`
	Schedule       string     `json:"schedule"`
	DailyTime      string     `json:"daily_time"`
	ConsumerWeight int        `json:"consumer_weight"`
	SuccessWeight  int        `json:"success_weight"`
	TTFTWeight     int        `json:"ttft_weight"`
	CacheWeight    int        `json:"cache_weight"`
	Explore        int        `json:"explore"`
	LastBuild      *time.Time `json:"last_build_at"`
	LastError      string     `json:"last_error"`
}

// buildCandidate is a scored channel group considered as an auto-build pool member.
type buildCandidate struct {
	group    string
	score    float64
	factor   int64
	requests int64
}

func (s *Service) BuildPool(ctx context.Context, user int64, id string) (RoutePool, error) {
	p, config, build, err := s.loadPoolAutoBuildTx(ctx, user, id)
	if err != nil {
		return p, err
	}
	candidates, err := s.scoreBuildCandidatesTx(ctx, user, build)
	if err != nil {
		return p, err
	}
	candidates = selectBuildCandidates(candidates, build)
	p.Members = []PoolMember{}
	for i, c := range candidates {
		p.Members = append(p.Members, PoolMember{c.group, i})
	}
	p.Config, err = finalizeAutoBuild(config, &build, s.cfg.Now())
	if err != nil {
		return p, err
	}
	return s.SavePool(ctx, user, p)
}

// loadPoolAutoBuildTx locates the user's pool by id and parses its
// auto_build config, applying the same defaults as the original inline code.
func (s *Service) loadPoolAutoBuildTx(ctx context.Context, user int64, id string) (RoutePool, map[string]json.RawMessage, autoBuild, error) {
	pools, err := s.Pools(ctx, user)
	if err != nil {
		return RoutePool{}, nil, autoBuild{}, err
	}
	var p RoutePool
	found := false
	for _, pool := range pools {
		if pool.ID == id {
			p = pool
			found = true
			break
		}
	}
	if !found {
		return p, nil, autoBuild{}, ErrNotFound
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(p.Config, &config) != nil {
		return p, nil, autoBuild{}, ErrInvalid
	}
	var build autoBuild
	if raw := config["auto_build"]; len(raw) > 0 {
		if json.Unmarshal(raw, &build) != nil {
			return p, nil, autoBuild{}, ErrInvalid
		}
	}
	if build.Size <= 0 || build.Size > 10 {
		build.Size = 3
	}
	if build.Model != "" {
		build.Models = append(build.Models, build.Model)
	}
	return p, config, build, nil
}

// scoreBuildCandidatesTx scores every channel group the user can see against
// the auto-build weighting, skipping groups that don't serve a required model.
func (s *Service) scoreBuildCandidatesTx(ctx context.Context, user int64, build autoBuild) ([]buildCandidate, error) {
	channels, err := s.List(ctx, Actor{UserID: user}, false)
	if err != nil {
		return nil, err
	}
	var candidates []buildCandidate
	for _, c := range channels {
		allowed := len(build.Models) == 0
		for _, needed := range build.Models {
			for _, m := range c.Models {
				if strings.EqualFold(m, needed) {
					allowed = true
				}
			}
		}
		if !allowed {
			continue
		}
		var success, cache, ttft float64
		var requests, avg int64
		err = s.pool.QueryRow(ctx, `SELECT coalesce(r.wilson_success_rate,0),coalesce(r.cache_hit_rate,0),coalesce(r.avg_ttft_ms,0),coalesce(r.request_count,0),coalesce(r.avg_consumer_micro,0) FROM (SELECT 1) d LEFT JOIN LATERAL(SELECT * FROM v3_channelmarket.ranking_snapshots WHERE group_id=$1 ORDER BY calculated_at DESC LIMIT 1) r ON true`, c.GroupID).Scan(&success, &cache, &ttft, &requests, &avg)
		if err != nil {
			return nil, err
		}
		if build.ConsumerWeight+build.SuccessWeight+build.TTFTWeight+build.CacheWeight == 0 {
			build.ConsumerWeight = 25
			build.SuccessWeight = 35
			build.TTFTWeight = 20
			build.CacheWeight = 20
		}
		cost := float64(0)
		if avg > 0 {
			cost = 1 / (1 + float64(avg)/1000000)
		}
		latency := float64(0)
		if ttft > 0 {
			latency = 1 / (1 + ttft/1000)
		}
		score := cost*float64(build.ConsumerWeight) + success*float64(build.SuccessWeight) + latency*float64(build.TTFTWeight) + cache*float64(build.CacheWeight)
		candidates = append(candidates, buildCandidate{c.GroupID, score, c.MultiplierPPM, requests})
	}
	return candidates, nil
}

// selectBuildCandidates ranks candidates by score and, when there are more
// than build.Size, carves out build.Explore low-traffic slots for exploration.
func selectBuildCandidates(candidates []buildCandidate, build autoBuild) []buildCandidate {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			if candidates[i].factor == candidates[j].factor {
				return candidates[i].group < candidates[j].group
			}
			return candidates[i].factor < candidates[j].factor
		}
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) <= build.Size {
		return candidates
	}
	explore := build.Explore
	if explore < 0 || explore >= build.Size {
		explore = 0
	}
	ranked := append([]buildCandidate(nil), candidates[:build.Size-explore]...)
	rest := append([]buildCandidate(nil), candidates[build.Size-explore:]...)
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].requests == rest[j].requests {
			return rest[i].group < rest[j].group
		}
		return rest[i].requests < rest[j].requests
	})
	ranked = append(ranked, rest[:explore]...)
	return ranked
}

// finalizeAutoBuild computes the next scheduled build time, stamps the
// build metadata, and re-marshals the pool config with the updated auto_build
// and last_built_at entries.
func finalizeAutoBuild(config map[string]json.RawMessage, build *autoBuild, now time.Time) (json.RawMessage, error) {
	if build.Interval <= 0 {
		build.Interval = 60
	}
	next := now.Add(time.Duration(build.Interval) * time.Minute)
	if build.Schedule == "daily" {
		clock, e := time.Parse("15:04", build.DailyTime)
		if e != nil {
			return nil, ErrInvalid
		}
		next = time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
	}
	build.Next = &next
	build.LastBuild = &now
	build.LastError = ""
	payload, err := json.Marshal(build)
	if err != nil {
		return nil, err
	}
	config["auto_build"] = payload
	config["last_built_at"], err = json.Marshal(now)
	if err != nil {
		return nil, err
	}
	return json.Marshal(config)
}

func (s *Service) RebuildPools(ctx context.Context, limit int) (int, error) {
	if s.pool == nil {
		return 0, ErrUnavailable
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `SELECT id,owner_user_id FROM v3_channelmarket.route_pools WHERE config->'auto_build'->>'enabled'='true' AND (config->'auto_build'->>'next_build_at' IS NULL OR (config->'auto_build'->>'next_build_at')::timestamptz<=$1) ORDER BY id LIMIT $2`, s.cfg.Now(), limit)
	if err != nil {
		return 0, err
	}
	type item struct {
		id   string
		user int64
	}
	var items []item
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.user); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, i)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	for i, item := range items {
		if _, err = s.BuildPool(ctx, item.user, item.id); err != nil {
			return i, err
		}
	}
	return len(items), nil
}
