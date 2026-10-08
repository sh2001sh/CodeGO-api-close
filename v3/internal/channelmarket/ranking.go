package channelmarket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// RefreshRankings uses persisted request outcomes and billing. Unmeasured latency/throughput stays
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
	rows, e := tx.Query(ctx, `WITH usage AS (
	SELECT channel_id,user_id,request_id,sum(amount) AS amount,max(cached_tokens) AS cached_tokens,max(prompt_tokens) AS prompt_tokens,
	bool_or(terminal IN ('success','succeeded','completed','Completed','completed_no_usage','CompletedNoUsage')) AS successful
	FROM v3_billing.usage_logs WHERE created_at>=$1 AND created_at<=$2 GROUP BY channel_id,user_id,request_id),
	requests AS (
	SELECT a.final_channel_id AS channel_id,a.user_id,a.request_id,
	coalesce(u.amount,0) AS amount,coalesce(u.cached_tokens,0) AS cached_tokens,coalesce(u.prompt_tokens,0) AS prompt_tokens,
	a.status IN ('success','succeeded') AS successful
	FROM v3_audit.request_audits a LEFT JOIN usage u ON u.channel_id=a.final_channel_id AND u.request_id=a.request_id
	WHERE a.started_at>=$1 AND a.started_at<=$2 AND a.completed_at<=$2 AND a.counted_in_success_rate
	UNION ALL
	SELECT u.channel_id,u.user_id,u.request_id,u.amount,u.cached_tokens,u.prompt_tokens,u.successful FROM usage u
	WHERE NOT EXISTS(SELECT 1 FROM v3_audit.request_audits a WHERE a.request_id=u.request_id AND a.status<>'historical_unknown'))
	SELECT g.id,g.channel_id,g.source_label,g.multiplier_ppm,
	count(l.request_id),count(l.request_id) FILTER(WHERE l.successful),
	count(DISTINCT l.user_id),coalesce(round(avg(l.amount)),0)::bigint,
	coalesce(sum(l.cached_tokens)::numeric/nullif(sum(l.prompt_tokens),0),0)::float8,
	ARRAY(SELECT model FROM v3_catalog.channel_models WHERE channel_id=g.channel_id ORDER BY model)
	FROM v3_channelmarket.groups g LEFT JOIN requests l ON l.channel_id=g.channel_id
	WHERE g.deleted_at IS NULL GROUP BY g.id ORDER BY g.id`, now.Add(-24*time.Hour), now)
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
	p, _, err := s.buildPool(ctx, user, id, false)
	return p, err
}

// Lock the pool for the entire build so concurrent workers and owner edits
// cannot overwrite each other's settings or member selection.
func (s *Service) buildPool(ctx context.Context, user int64, id string, dueOnly bool) (RoutePool, bool, error) {
	var p RoutePool
	built := false
	var resultErr error
	var failureConfig json.RawMessage
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		var maximum int64
		var internal string
		e := tx.QueryRow(ctx, `SELECT id,owner_user_id,name,internal_group_name,strategy,max_attempts,failure_cooldown_seconds,max_multiplier_ppm,config FROM v3_channelmarket.route_pools WHERE id=$1 AND owner_user_id=$2 FOR UPDATE`, id, user).Scan(&p.ID, &p.OwnerUserID, &p.Name, &internal, &p.Strategy, &p.MaxAttempts, &p.FailureCooldownSeconds, &maximum, &p.Config)
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		p.MaxMultiplier, p.TokenGroup = json.Number(formatFactor(maximum)), internal
		failureConfig = append(json.RawMessage(nil), p.Config...)
		var config map[string]json.RawMessage
		if json.Unmarshal(p.Config, &config) != nil {
			return ErrInvalid
		}
		if len(config["auto_build"]) == 0 {
			config["auto_build"] = json.RawMessage(`{}`)
		}
		build, e := parseAutoBuild(config["auto_build"], true)
		if e != nil {
			return e
		}
		now := s.cfg.Now().UTC()
		if dueOnly && (!build.Enabled || (build.Next != nil && build.Next.After(now))) {
			return nil
		}
		candidates, e := scoreBuildCandidatesTx(ctx, tx, user, build, maximum, now)
		if e != nil {
			return e
		}
		if len(candidates) == 0 {
			// A temporary lack of candidates must not erase an otherwise useful
			// pool or its API Key bindings. The worker will retry later.
			build.LastError = "No eligible groups match the selected models and multiplier limit"
			next := now.Add(15 * time.Minute)
			build.Next = &next
			config["auto_build"], e = json.Marshal(build)
			if e == nil {
				p.Config, e = json.Marshal(config)
			}
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `UPDATE v3_channelmarket.route_pools SET config=$3 WHERE id=$1 AND owner_user_id=$2`, id, user, p.Config); e != nil {
				return e
			}
			resultErr = ErrConflict
			return nil
		}
		candidates = selectBuildCandidates(candidates, build)
		p.Members = make([]PoolMember, 0, len(candidates))
		for i, c := range candidates {
			p.Members = append(p.Members, PoolMember{GroupID: c.group, Priority: i})
		}
		p.Config, e = finalizeAutoBuild(config, &build, now)
		if e != nil {
			return e
		}
		_, catalogPool, e := upsertPoolRowTx(ctx, tx, user, p, maximum, false)
		if e != nil {
			return e
		}
		if e = syncPoolMembersTx(ctx, tx, user, p, maximum, internal, catalogPool, now); e != nil {
			return e
		}
		p.AutoBuild = config["auto_build"]
		built = true
		return nil
	})
	if err != nil && p.ID != "" {
		// Do not persist database diagnostics in owner-visible JSON.
		if recordErr := s.recordPoolBuildFailure(ctx, user, id, failureConfig); recordErr != nil {
			err = errors.Join(err, recordErr)
		}
	}
	return p, built, errors.Join(err, resultErr)
}

// ANY selected model may be served by a member, matching the original pool;
// the gateway filters each member again against the actual requested model.
func scoreBuildCandidatesTx(ctx context.Context, tx pgx.Tx, user int64, build autoBuild, maximum int64, now time.Time) ([]buildCandidate, error) {
	rows, err := tx.Query(ctx, `SELECT g.id,
	coalesce(u.multiplier_ppm,least(g.multiplier_ppm,coalesce((SELECT min(w.multiplier_ppm) FROM v3_channelmarket.time_range_multipliers w WHERE w.channel_id=c.id AND w.starts_at<=$2 AND w.ends_at>$2),g.multiplier_ppm))),
	ARRAY(SELECT model FROM v3_catalog.channel_models WHERE channel_id=c.id ORDER BY model),
	coalesce(r.wilson_success_rate,0),coalesce(r.cache_hit_rate,0),coalesce(r.request_count,0),coalesce(r.avg_consumer_micro,0)
	FROM v3_channelmarket.groups g JOIN v3_catalog.channels c ON c.id=g.channel_id
	LEFT JOIN v3_channelmarket.user_multipliers u ON u.channel_id=c.id AND u.user_id=$1
	LEFT JOIN LATERAL(SELECT * FROM v3_channelmarket.ranking_snapshots WHERE group_id=g.id AND window_hours=24 AND ranking_version='v3-usage' AND calculated_at>=$2::timestamptz-interval '1 hour' AND calculated_at<=$2::timestamptz+interval '5 minutes' ORDER BY calculated_at DESC LIMIT 1) r ON true
	WHERE g.deleted_at IS NULL AND g.lifecycle_status='active' AND c.status='enabled'
	AND EXISTS(SELECT 1 FROM v3_catalog.channel_credentials k WHERE k.channel_id=c.id AND k.status='enabled')
	AND (g.visibility='public' OR g.owner_user_id=$1 OR EXISTS(SELECT 1 FROM v3_channelmarket.group_access a WHERE a.group_id=g.id AND a.user_id=$1))
	AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=c.id AND b.user_id=$1)
	UNION ALL
	SELECT 'official:'||g.name,round(g.multiplier*1000000)::bigint,
	ARRAY(SELECT DISTINCT cm.model FROM v3_catalog.channels c JOIN v3_catalog.channel_groups cg ON cg.channel_id=c.id JOIN v3_catalog.channel_models cm ON cm.channel_id=c.id WHERE cg.group_name=g.name AND c.scope='official' AND c.status='enabled' AND EXISTS(SELECT 1 FROM v3_catalog.channel_credentials k WHERE k.channel_id=c.id AND k.status='enabled') ORDER BY cm.model),
	0::double precision,0::double precision,0::bigint,0::bigint
	FROM v3_catalog.groups g WHERE EXISTS(SELECT 1 FROM v3_identity.allowed_groups($1) a WHERE a=g.name)`, user, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []buildCandidate
	for rows.Next() {
		var id string
		var factor int64
		var models []string
		var success, cache float64
		var requests, avg int64
		if err = rows.Scan(&id, &factor, &models, &success, &cache, &requests, &avg); err != nil {
			return nil, err
		}
		if len(models) == 0 || !matchesPoolModels(models, build.Models) || (maximum > 0 && factor > maximum) {
			continue
		}
		cost := float64(0)
		if requests > 0 && avg >= 0 {
			cost = 1 / (1 + float64(avg)/1000000)
		}
		score := cost*float64(build.ConsumerWeight) + success*float64(build.SuccessWeight) + cache*float64(build.CacheWeight)
		candidates = append(candidates, buildCandidate{id, score, factor, requests})
	}
	return candidates, rows.Err()
}

func matchesPoolModels(models, required []string) bool {
	if len(required) == 0 {
		return true
	}
	for _, needed := range required {
		for _, model := range models {
			if strings.EqualFold(model, needed) {
				return true
			}
		}
	}
	return false
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
	now = now.UTC()
	build.Next = nil
	if build.Enabled {
		next := nextPoolBuild(*build, now)
		build.Next = &next
	}
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
	rows, err := s.pool.Query(ctx, `SELECT id,owner_user_id,config->'auto_build' FROM v3_channelmarket.route_pools WHERE config->'auto_build'->>'enabled'='true' ORDER BY config->'auto_build'->>'next_build_at' NULLS FIRST,id`)
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
		var raw []byte
		if err = rows.Scan(&i.id, &i.user, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		var build autoBuild
		if json.Unmarshal(raw, &build) == nil && build.Next != nil && build.Next.After(s.cfg.Now()) {
			continue
		}
		items = append(items, i)
		if len(items) == limit {
			break
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	count := 0
	var failures []error
	for _, item := range items {
		if _, built, e := s.buildPool(ctx, item.user, item.id, true); e != nil {
			failures = append(failures, fmt.Errorf("pool %s: %w", item.id, e))
		} else if built {
			count++
		}
	}
	return count, errors.Join(failures...)
}

func (s *Service) recordPoolBuildFailure(ctx context.Context, user int64, id string, expected json.RawMessage) error {
	next := s.cfg.Now().UTC().Add(15 * time.Minute)
	_, err := s.pool.Exec(ctx, `UPDATE v3_channelmarket.route_pools SET config=jsonb_set(config,'{auto_build}',coalesce(config->'auto_build','{}')||jsonb_build_object('last_error','Automatic pool update failed; retry scheduled','next_build_at',$3::timestamptz)) WHERE id=$1 AND owner_user_id=$2 AND config=$4`, id, user, next, expected)
	return err
}
