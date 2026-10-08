package channelmarket

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type ModelFailureCount struct {
	Category string `json:"category"`
	Count    int64  `json:"count"`
}

// Rates are fractions (0..1); latency is milliseconds and throughput is output
// tokens/second over measured generation time. NULL means no usable measurement.
type ChannelMarketInsights struct {
	GroupID              string                   `json:"group_id"`
	DisplayID            string                   `json:"display_id"`
	Model                string                   `json:"model"`
	WindowHours          int                      `json:"window_hours"`
	RequestCount         int64                    `json:"request_count"`
	SuccessCount         int64                    `json:"success_count"`
	IndependentConsumers int64                    `json:"independent_consumers"`
	SuccessRate          *float64                 `json:"success_rate,omitempty"`
	WilsonSuccessRate    *float64                 `json:"wilson_success_rate,omitempty"`
	TTFTP50MS            *float64                 `json:"ttft_p50_ms,omitempty"`
	TTFTP95MS            *float64                 `json:"ttft_p95_ms,omitempty"`
	AverageTPS           *float64                 `json:"avg_tps,omitempty"`
	PerformanceSamples   int64                    `json:"performance_samples"`
	Disclosure           *ChannelMarketDisclosure `json:"disclosure,omitempty"`
	FailureCounts        []ModelFailureCount      `json:"failure_counts"`
}

const modelObservationsSQL = `WITH observations AS (
 SELECT a.request_id,a.user_id,a.status IN ('success','succeeded') AS successful,
 a.status_code,a.error_code,a.ttft_ms,a.completion_tokens,a.generation_ms
 FROM v3_audit.request_audits a
 WHERE a.final_channel_id=$1 AND a.model=$2 AND a.started_at>=$3 AND a.started_at<=$4
 AND a.completed_at<=$4 AND a.counted_in_success_rate
 UNION ALL
 SELECT l.request_id,l.user_id,
 bool_or(l.terminal IN ('success','succeeded','completed','Completed','completed_no_usage','CompletedNoUsage')),
 0::bigint,''::text,NULL::bigint,NULL::bigint,NULL::bigint
 FROM v3_billing.usage_logs l
 WHERE l.channel_id=$1 AND l.model=$2 AND l.created_at>=$3 AND l.created_at<=$4
 AND NOT EXISTS(SELECT 1 FROM v3_audit.request_audits a WHERE a.request_id=l.request_id)
 GROUP BY l.request_id,l.user_id)
`

func (s *Service) ModelInsights(ctx context.Context, a Actor, identity, model string, hours int) (ChannelMarketInsights, error) {
	result := ChannelMarketInsights{Model: model, WindowHours: hours, FailureCounts: []ModelFailureCount{}}
	if identity == "" || !validDisclosureModel(model) || (hours != 24 && hours != 168) {
		return result, ErrInvalid
	}
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		var channel, owner int64
		err := tx.QueryRow(ctx, `SELECT g.id,g.public_channel_id,g.channel_id,g.owner_user_id
 FROM v3_channelmarket.groups g JOIN v3_catalog.channels c ON c.id=g.channel_id
 WHERE (g.id=$1 OR g.public_slug=$1 OR g.public_channel_id=$1) AND g.deleted_at IS NULL
 AND ($2 OR g.owner_user_id=$3 OR (g.lifecycle_status='active' AND c.status='enabled'
 AND (g.visibility='public' OR EXISTS(SELECT 1 FROM v3_channelmarket.group_access ga WHERE ga.group_id=g.id AND ga.user_id=$3))
 AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=c.id AND b.user_id=$3)))
 AND EXISTS(SELECT 1 FROM v3_catalog.channel_models cm WHERE cm.channel_id=g.channel_id AND cm.model=$4)`, identity, a.Admin, a.UserID, model).Scan(&result.GroupID, &result.DisplayID, &channel, &owner)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		now := s.cfg.Now()
		args := []any{channel, model, now.Add(-time.Duration(hours) * time.Hour), now, owner}
		err = tx.QueryRow(ctx, modelObservationsSQL+`SELECT count(*),count(*) FILTER(WHERE successful),
 count(DISTINCT user_id) FILTER(WHERE user_id>0 AND user_id<>$5),
 percentile_cont(0.5) WITHIN GROUP(ORDER BY ttft_ms) FILTER(WHERE successful AND ttft_ms>0),
 percentile_cont(0.95) WITHIN GROUP(ORDER BY ttft_ms) FILTER(WHERE successful AND ttft_ms>0),
 avg(completion_tokens::float8*1000/generation_ms) FILTER(WHERE successful AND completion_tokens>0 AND generation_ms>0),
 count(*) FILTER(WHERE successful AND ttft_ms>0) FROM observations`, args...).Scan(
			&result.RequestCount, &result.SuccessCount, &result.IndependentConsumers, &result.TTFTP50MS, &result.TTFTP95MS, &result.AverageTPS, &result.PerformanceSamples)
		if err != nil {
			return err
		}
		if result.RequestCount > 0 {
			rate, bound := float64(result.SuccessCount)/float64(result.RequestCount), wilson(result.SuccessCount, result.RequestCount)
			result.SuccessRate, result.WilsonSuccessRate = &rate, &bound
		}
		// Expose stable categories, never raw upstream errors or user/request IDs.
		rows, err := tx.Query(ctx, modelObservationsSQL+`SELECT CASE
 WHEN status_code=429 OR error_code IN ('rate_limit_exceeded','rate_limited','upstream_rate_limit') THEN 'rate_limited'
 WHEN status_code IN (408,504) OR error_code IN ('timeout','upstream_timeout','request_timeout','context_deadline_exceeded') THEN 'timeout'
 WHEN status_code IN (401,403) THEN 'upstream_auth'
 WHEN status_code>=500 THEN 'upstream_error'
 WHEN status_code>=400 THEN 'rejected'
 ELSE 'other' END AS category,count(*)::bigint
 FROM observations WHERE NOT successful GROUP BY 1 ORDER BY count(*) DESC,1`, args[:4]...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var failure ModelFailureCount
			if err = rows.Scan(&failure.Category, &failure.Count); err != nil {
				rows.Close()
				return err
			}
			result.FailureCounts = append(result.FailureCounts, failure)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		result.Disclosure, err = readDisclosureTx(ctx, tx, channel)
		return err
	})
	return result, err
}
