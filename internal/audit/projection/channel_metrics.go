package projection

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

var channelHotBuckets sync.Map

func recordChannelHotBucket(sample Sample, bucketTs int64) {
	if sample.ChannelID <= 0 {
		return
	}
	key := channelBucketKey{channelID: sample.ChannelID, bucketTs: bucketTs}
	actual, _ := channelHotBuckets.LoadOrStore(key, &atomicBucket{})
	actual.(*atomicBucket).add(sample)
}

func flushCompletedChannelBuckets(currentBucket int64) {
	channelHotBuckets.Range(func(key, value any) bool {
		bucketKey := key.(channelBucketKey)
		if bucketKey.bucketTs >= currentBucket {
			return true
		}
		bucket := value.(*atomicBucket)
		drained := bucket.drain()
		if drained.requestCount == 0 {
			deleteOldChannelBucket(bucketKey, key)
			return true
		}
		err := upsertChannelMetric(&channelPerfMetricRecord{
			ChannelID: bucketKey.channelID, BucketTs: bucketKey.bucketTs,
			RequestCount: drained.requestCount, SuccessCount: drained.successCount,
			TotalLatencyMs: drained.totalLatencyMs, TtftSumMs: drained.ttftSumMs,
			TtftCount: drained.ttftCount, OutputTokens: drained.outputTokens,
			GenerationMs: drained.generationMs,
		})
		if err != nil {
			bucket.addCounters(drained)
			platformobservability.SysError(fmt.Sprintf("failed to flush channel perf metric channel=%d bucket=%d: %s", bucketKey.channelID, bucketKey.bucketTs, err.Error()))
			return true
		}
		deleteOldChannelBucket(bucketKey, key)
		return true
	})
}

func deleteOldChannelBucket(key channelBucketKey, rawKey any) {
	if key.bucketTs < bucketStart(time.Now().Add(-24*time.Hour).Unix()) {
		channelHotBuckets.Delete(rawKey)
	}
}

// QuerySummaryByChannels returns site-wide performance aggregated across all
// models and request groups for each requested channel.
func QuerySummaryByChannels(hours int, channelIDs []int) ([]ChannelSummary, error) {
	if len(channelIDs) == 0 {
		return []ChannelSummary{}, nil
	}
	if hours <= 0 {
		hours = 24
	}
	if hours > 24*30 {
		hours = 24 * 30
	}
	endTs := time.Now().Unix()
	startTs := endTs - int64(hours)*3600
	rows, err := getChannelPerfMetricSummaries(startTs, endTs, channelIDs)
	if err != nil {
		return nil, err
	}
	totals := make(map[int]counters, len(channelIDs))
	for _, row := range rows {
		totals[row.ChannelID] = counters{
			requestCount: row.RequestCount, successCount: row.SuccessCount,
			totalLatencyMs: row.TotalLatencyMs, ttftSumMs: row.TtftSumMs,
			ttftCount: row.TtftCount, outputTokens: row.OutputTokens,
			generationMs: row.GenerationMs,
		}
	}
	if platformcache.RedisEnabled && platformcache.RDB != nil {
		mergeActiveChannelRedisBuckets(totals, channelIDs, bucketStart(endTs))
	} else {
		mergeChannelHotBuckets(totals, channelIDs, startTs, endTs)
	}
	return buildChannelSummaries(totals), nil
}

func mergeChannelHotBuckets(totals map[int]counters, channelIDs []int, startTs, endTs int64) {
	allowed := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		allowed[channelID] = struct{}{}
	}
	channelHotBuckets.Range(func(key, value any) bool {
		bucketKey := key.(channelBucketKey)
		if bucketKey.bucketTs < startTs || bucketKey.bucketTs > endTs {
			return true
		}
		if _, ok := allowed[bucketKey.channelID]; !ok {
			return true
		}
		current := totals[bucketKey.channelID]
		mergeCounterValue(&current, value.(*atomicBucket).snapshot())
		totals[bucketKey.channelID] = current
		return true
	})
}

type channelRedisCommand struct {
	channelID int
	command   *redis.StringStringMapCmd
}

func mergeActiveChannelRedisBuckets(totals map[int]counters, channelIDs []int, bucketTs int64) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pipe := platformcache.RDB.Pipeline()
	commands := make([]channelRedisCommand, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			continue
		}
		commands = append(commands, channelRedisCommand{
			channelID: channelID,
			command:   pipe.HGetAll(ctx, channelRedisBucketKey(channelID, bucketTs)),
		})
	}
	_, _ = pipe.Exec(ctx)
	for _, item := range commands {
		values, err := item.command.Result()
		if err != nil || len(values) == 0 {
			continue
		}
		current := totals[item.channelID]
		mergeCounterValue(&current, redisCounters(values))
		totals[item.channelID] = current
	}
}

func mergeCounterValue(target *counters, value counters) {
	target.requestCount += value.requestCount
	target.successCount += value.successCount
	target.totalLatencyMs += value.totalLatencyMs
	target.ttftSumMs += value.ttftSumMs
	target.ttftCount += value.ttftCount
	target.outputTokens += value.outputTokens
	target.generationMs += value.generationMs
}

func buildChannelSummaries(totals map[int]counters) []ChannelSummary {
	results := make([]ChannelSummary, 0, len(totals))
	for channelID, total := range totals {
		if total.requestCount == 0 {
			continue
		}
		results = append(results, ChannelSummary{
			ChannelID: channelID, AvgLatencyMs: avg(total.totalLatencyMs, total.requestCount),
			AvgTtftMs:   avg(total.ttftSumMs, total.ttftCount),
			SuccessRate: math.Round(successRate(total)*100) / 100,
			AvgTps:      math.Round(avgTps(total)*100) / 100, RequestCount: total.requestCount,
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ChannelID < results[j].ChannelID })
	return results
}

func channelRedisBucketKey(channelID int, bucketTs int64) string {
	return fmt.Sprintf("perf:channel:%d:%d", channelID, bucketTs)
}
