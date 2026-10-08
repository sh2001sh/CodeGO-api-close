package channelmarket

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

const recentBucketSeconds int64 = 3600
const recentBucketCount = 6

type RecentRequestBucket struct {
	Ts           int64   `json:"ts"`
	RequestCount int64   `json:"request_count"`
	SuccessRate  float64 `json:"success_rate"`
}

type recentQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func emptyRecentRequests(now time.Time) []RecentRequestBucket {
	start := now.Unix()/recentBucketSeconds*recentBucketSeconds - (recentBucketCount-1)*recentBucketSeconds
	result := make([]RecentRequestBucket, recentBucketCount)
	for i := range result {
		result[i].Ts = start + int64(i)*recentBucketSeconds
	}
	return result
}

// Query only the already-authorized channels, once for the whole market page.
// Terminal request metadata includes uncharged failures; usage rows supplement
// older/billing-only records, never duplicate a request with an audit record.
// This projection returns counts only, with no request/user/key/payload data.
func attachRecentRequests(ctx context.Context, queryer recentQueryer, groups []ChannelView, now time.Time) error {
	if len(groups) == 0 {
		return nil
	}
	positions := make(map[int64]int, len(groups))
	ids := make([]int64, 0, len(groups))
	for i := range groups {
		groups[i].RecentBucketSeconds = recentBucketSeconds
		groups[i].RecentRequests = emptyRecentRequests(now)
		if _, found := positions[groups[i].InternalChannelID]; !found {
			positions[groups[i].InternalChannelID] = i
			ids = append(ids, groups[i].InternalChannelID)
		}
	}
	start := time.Unix(groups[0].RecentRequests[0].Ts, 0)
	// Channel sets vary from one detail to the full catalog. Plan for this set;
	// cached generic plans underestimate hot channels and repeatedly probe audits.
	rows, err := queryer.Query(ctx, `WITH observations AS (
	SELECT a.final_channel_id AS channel_id,a.request_id,a.started_at AS observed_at,a.status IN ('success','succeeded') AS successful
	FROM v3_audit.request_audits a
	WHERE a.final_channel_id=ANY($1::bigint[]) AND a.started_at>=$2 AND a.started_at<=$3 AND a.completed_at<=$3 AND a.counted_in_success_rate
	UNION ALL
	SELECT l.channel_id,l.request_id,min(l.created_at),bool_or(l.terminal IN ('success','succeeded','completed','Completed','completed_no_usage','CompletedNoUsage'))
	FROM v3_billing.usage_logs l
	WHERE l.channel_id=ANY($1::bigint[]) AND l.created_at>=$2 AND l.created_at<=$3
	AND NOT EXISTS(SELECT 1 FROM v3_audit.request_audits a WHERE a.request_id=l.request_id)
	GROUP BY l.channel_id,l.request_id)
	SELECT channel_id,extract(epoch FROM date_bin('1 hour',observed_at,'1970-01-01 00:00:00+00'::timestamptz))::bigint AS bucket,
	count(*)::bigint,count(*) FILTER(WHERE successful)::bigint
	FROM observations GROUP BY channel_id,date_bin('1 hour',observed_at,'1970-01-01 00:00:00+00'::timestamptz)
	ORDER BY 1,2`, pgx.QueryExecModeExec, ids, start, now)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var channelID, bucket, count, success int64
		if err = rows.Scan(&channelID, &bucket, &count, &success); err != nil {
			return err
		}
		position, found := positions[channelID]
		if !found {
			continue
		}
		slot := (bucket - groups[position].RecentRequests[0].Ts) / recentBucketSeconds
		if slot < 0 || slot >= recentBucketCount || count <= 0 {
			continue
		}
		groups[position].RecentRequests[slot].RequestCount = count
		groups[position].RecentRequests[slot].SuccessRate = float64(success) / float64(count) * 100
	}
	return rows.Err()
}
