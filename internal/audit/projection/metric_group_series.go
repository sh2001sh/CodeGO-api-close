package projection

import (
	"context"
	"sort"
	"time"

	"github.com/go-redis/redis/v8"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
)

func QuerySeriesByGroupModels(hours int, groups []string) ([]GroupModelSeries, error) {
	startTs, endTs := metricWindow(hours)
	rows, err := getPerfMetricsBucketsByGroups(startTs, endTs, groups)
	if err != nil {
		return nil, err
	}
	buckets := make(map[modelGroupKey]map[int64]counters)
	for _, row := range rows {
		appendGroupModelBucket(buckets, modelGroupKey{group: row.Group, model: row.ModelName}, row.BucketTs, countersFromRecord(row))
	}
	allowed := groupSet(groups)
	redisBuckets := mergeRecentGroupModelRedisBuckets(buckets, startTs, endTs)
	mergeHotGroupModelBuckets(buckets, allowed, startTs, endTs, redisBuckets)
	return buildGroupModelSeries(buckets), nil
}

func countersFromRecord(row perfMetricRecord) counters {
	return counters{
		requestCount: row.RequestCount, successCount: row.SuccessCount,
		totalLatencyMs: row.TotalLatencyMs, ttftSumMs: row.TtftSumMs,
		ttftCount: row.TtftCount, outputTokens: row.OutputTokens,
		generationMs: row.GenerationMs, inputTokens: row.InputTokens,
		cacheReadTokens: row.CacheReadTokens, cacheWriteTokens: row.CacheWriteTokens,
	}
}

func appendGroupModelBucket(buckets map[modelGroupKey]map[int64]counters, key modelGroupKey, ts int64, value counters) {
	if value.requestCount == 0 {
		return
	}
	if buckets[key] == nil {
		buckets[key] = make(map[int64]counters)
	}
	current := buckets[key][ts]
	mergeCounterValue(&current, value)
	buckets[key][ts] = current
}

func mergeHotGroupModelBuckets(buckets map[modelGroupKey]map[int64]counters, allowed map[string]struct{}, startTs, endTs int64, redisBuckets map[bucketKey]struct{}) {
	hotBuckets.Range(func(key, value any) bool {
		metricKey := key.(bucketKey)
		if metricKey.bucketTs < startTs || metricKey.bucketTs > endTs || !groupAllowed(metricKey.group, allowed) {
			return true
		}
		if _, alreadyMerged := redisBuckets[metricKey]; alreadyMerged {
			return true
		}
		appendGroupModelBucket(buckets, modelGroupKey{group: metricKey.group, model: metricKey.model}, metricKey.bucketTs, value.(*atomicBucket).snapshot())
		return true
	})
}

type groupModelRedisCommand struct {
	key     bucketKey
	command *redis.StringStringMapCmd
}

// mergeRecentGroupModelRedisBuckets fills the active bucket and the previous
// bucket while it is waiting for the periodic database flush. Redis already
// contains process-wide aggregate counters, so status readers stay current
// without scanning raw request logs.
func mergeRecentGroupModelRedisBuckets(buckets map[modelGroupKey]map[int64]counters, startTs, endTs int64) map[bucketKey]struct{} {
	merged := make(map[bucketKey]struct{})
	if !platformcache.RedisEnabled || platformcache.RDB == nil || len(buckets) == 0 {
		return merged
	}

	active := bucketStart(time.Now().Unix())
	candidates := []int64{active, active - getPerfMetricsBucketSeconds()}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pipe := platformcache.RDB.Pipeline()
	commands := make([]groupModelRedisCommand, 0, len(buckets)*len(candidates))
	for modelGroup, values := range buckets {
		for _, bucketTs := range candidates {
			if bucketTs < startTs || bucketTs > endTs {
				continue
			}
			if _, persisted := values[bucketTs]; persisted {
				continue
			}
			key := bucketKey{model: modelGroup.model, group: modelGroup.group, bucketTs: bucketTs}
			commands = append(commands, groupModelRedisCommand{key: key, command: pipe.HGetAll(ctx, redisBucketKey(key))})
		}
	}
	_, _ = pipe.Exec(ctx)
	for _, item := range commands {
		values, err := item.command.Result()
		if err != nil || len(values) == 0 {
			continue
		}
		value := redisCounters(values)
		appendGroupModelBucket(buckets, modelGroupKey{group: item.key.group, model: item.key.model}, item.key.bucketTs, value)
		if value.requestCount > 0 {
			merged[item.key] = struct{}{}
		}
	}
	return merged
}

func buildGroupModelSeries(buckets map[modelGroupKey]map[int64]counters) []GroupModelSeries {
	results := make([]GroupModelSeries, 0, len(buckets))
	for key, values := range buckets {
		total, points := summarizeMetricBuckets(values)
		if total.requestCount == 0 {
			continue
		}
		results = append(results, GroupModelSeries{Group: key.group, ModelName: key.model,
			SuccessRate: roundMetric(successRate(total)), CacheHitRate: roundMetric(cacheHitRate(total)),
			RequestCount: total.requestCount, Series: points})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Group != results[j].Group {
			return results[i].Group < results[j].Group
		}
		return results[i].ModelName < results[j].ModelName
	})
	return results
}

func summarizeMetricBuckets(buckets map[int64]counters) (counters, []BucketPoint) {
	timestamps := make([]int64, 0, len(buckets))
	for ts := range buckets {
		timestamps = append(timestamps, ts)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })
	total := counters{}
	series := make([]BucketPoint, 0, len(timestamps))
	for _, ts := range timestamps {
		value := buckets[ts]
		mergeCounterValue(&total, value)
		series = append(series, bucketPoint(ts, value))
	}
	return total, series
}
