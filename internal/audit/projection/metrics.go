package projection

import (
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"math"
	"sort"
	"sync"
	"time"
)

var hotBuckets sync.Map

// seriesSchema is a stable client cache/schema marker. Do not change it when
// hiding fields or making response-only privacy hardening changes.
const seriesSchema = "dbcd0a3c01b55203"

func Init() {
	go flushLoop()
}

func RecordRelaySample(info *relaycommon.RelayInfo, success bool, outputTokens int64) {
	RecordRelayUsageSample(info, success, 0, 0, 0, outputTokens)
}

func RecordRelayUsageSample(info *relaycommon.RelayInfo, success bool, inputTokens, cacheReadTokens, cacheWriteTokens, outputTokens int64) {
	if info == nil {
		return
	}
	now := time.Now()
	hasTtft := info.IsStream && info.HasSendResponse()
	ttftMs := int64(0)
	if hasTtft {
		ttftMs = info.FirstResponseTime.Sub(info.StartTime).Milliseconds()
	}
	latencyMs := now.Sub(info.StartTime).Milliseconds()
	generationMs := latencyMs
	if hasTtft {
		generationMs = now.Sub(info.FirstResponseTime).Milliseconds()
	}
	if generationMs <= 0 {
		generationMs = latencyMs
	}
	Record(Sample{
		Model:            info.OriginModelName,
		Group:            info.UsingGroup,
		ChannelID:        info.ChannelId,
		LatencyMs:        latencyMs,
		TtftMs:           ttftMs,
		HasTtft:          hasTtft,
		Success:          success,
		OutputTokens:     outputTokens,
		GenerationMs:     generationMs,
		InputTokens:      inputTokens,
		CacheReadTokens:  cacheReadTokens,
		CacheWriteTokens: cacheWriteTokens,
	})
}

func Record(sample Sample) {
	setting := getPerfMetricsSetting()
	if !setting.Enabled || sample.Model == "" {
		return
	}
	if sample.Group == "" {
		sample.Group = "default"
	}
	if sample.LatencyMs < 0 {
		sample.LatencyMs = 0
	}

	bucketTs := bucketStart(time.Now().Unix())
	key := bucketKey{
		model:    sample.Model,
		group:    sample.Group,
		bucketTs: bucketTs,
	}
	actual, _ := hotBuckets.LoadOrStore(key, &atomicBucket{})
	actual.(*atomicBucket).add(sample)
	recordChannelHotBucket(sample, bucketTs)
	recordRedis(key, sample)
}

func Query(params QueryParams) (QueryResult, error) {
	if params.Hours <= 0 {
		params.Hours = 24
	}
	if params.Hours > 24*30 {
		params.Hours = 24 * 30
	}
	endTs := time.Now().Unix()
	startTs := endTs - int64(params.Hours)*3600

	merged := map[bucketKey]counters{}
	rows, err := getPerfMetrics(params.Model, params.Group, startTs, endTs)
	if err != nil {
		return QueryResult{}, err
	}
	for _, row := range rows {
		mergeCounters(merged, bucketKey{
			model:    row.ModelName,
			group:    row.Group,
			bucketTs: row.BucketTs,
		}, counters{
			requestCount:     row.RequestCount,
			successCount:     row.SuccessCount,
			totalLatencyMs:   row.TotalLatencyMs,
			ttftSumMs:        row.TtftSumMs,
			ttftCount:        row.TtftCount,
			outputTokens:     row.OutputTokens,
			generationMs:     row.GenerationMs,
			inputTokens:      row.InputTokens,
			cacheReadTokens:  row.CacheReadTokens,
			cacheWriteTokens: row.CacheWriteTokens,
		})
	}

	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if k.model != params.Model || k.bucketTs < startTs || k.bucketTs > endTs {
			return true
		}
		if params.Group != "" && k.group != params.Group {
			return true
		}
		mergeCounters(merged, k, value.(*atomicBucket).snapshot())
		return true
	})

	return buildQueryResult(params.Model, merged), nil
}

func bucketStart(ts int64) int64 {
	bucketSeconds := getPerfMetricsBucketSeconds()
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	return ts - (ts % bucketSeconds)
}

func mergeCounters(merged map[bucketKey]counters, key bucketKey, value counters) {
	if value.requestCount == 0 {
		return
	}
	current := merged[key]
	current.requestCount += value.requestCount
	current.successCount += value.successCount
	current.totalLatencyMs += value.totalLatencyMs
	current.ttftSumMs += value.ttftSumMs
	current.ttftCount += value.ttftCount
	current.outputTokens += value.outputTokens
	current.generationMs += value.generationMs
	current.inputTokens += value.inputTokens
	current.cacheReadTokens += value.cacheReadTokens
	current.cacheWriteTokens += value.cacheWriteTokens
	merged[key] = current
}

func buildQueryResult(modelName string, merged map[bucketKey]counters) QueryResult {
	groupBuckets := map[string]map[int64]counters{}
	for key, value := range merged {
		if value.requestCount == 0 {
			continue
		}
		if _, ok := groupBuckets[key.group]; !ok {
			groupBuckets[key.group] = map[int64]counters{}
		}
		groupBuckets[key.group][key.bucketTs] = value
	}

	groups := make([]string, 0, len(groupBuckets))
	for group := range groupBuckets {
		groups = append(groups, group)
	}
	sort.Strings(groups)

	results := make([]GroupResult, 0, len(groups))
	for _, group := range groups {
		buckets := groupBuckets[group]
		timestamps := make([]int64, 0, len(buckets))
		for ts := range buckets {
			timestamps = append(timestamps, ts)
		}
		sort.Slice(timestamps, func(i, j int) bool {
			return timestamps[i] < timestamps[j]
		})

		total := counters{}
		series := make([]BucketPoint, 0, len(timestamps))
		for _, ts := range timestamps {
			value := buckets[ts]
			total.requestCount += value.requestCount
			total.successCount += value.successCount
			total.totalLatencyMs += value.totalLatencyMs
			total.ttftSumMs += value.ttftSumMs
			total.ttftCount += value.ttftCount
			total.outputTokens += value.outputTokens
			total.generationMs += value.generationMs
			total.inputTokens += value.inputTokens
			total.cacheReadTokens += value.cacheReadTokens
			total.cacheWriteTokens += value.cacheWriteTokens
			series = append(series, bucketPoint(ts, value))
		}

		results = append(results, GroupResult{
			Group:            group,
			AvgTtftMs:        avg(total.ttftSumMs, total.ttftCount),
			AvgLatencyMs:     avg(total.totalLatencyMs, total.requestCount),
			SuccessRate:      successRate(total),
			AvgTps:           avgTps(total),
			CacheHitRate:     cacheHitRate(total),
			InputTokens:      total.inputTokens,
			CacheReadTokens:  total.cacheReadTokens,
			CacheWriteTokens: total.cacheWriteTokens,
			Series:           series,
		})
	}

	return QueryResult{
		ModelName:    modelName,
		SeriesSchema: seriesSchema,
		Groups:       results,
	}
}

func bucketPoint(ts int64, value counters) BucketPoint {
	return BucketPoint{
		Ts:               ts,
		AvgTtftMs:        avg(value.ttftSumMs, value.ttftCount),
		AvgLatencyMs:     avg(value.totalLatencyMs, value.requestCount),
		SuccessRate:      successRate(value),
		AvgTps:           avgTps(value),
		CacheHitRate:     cacheHitRate(value),
		InputTokens:      value.inputTokens,
		CacheReadTokens:  value.cacheReadTokens,
		CacheWriteTokens: value.cacheWriteTokens,
		RequestCount:     value.requestCount,
	}
}

func avg(sum int64, count int64) int64 {
	if count <= 0 {
		return 0
	}
	return sum / count
}

func successRate(value counters) float64 {
	if value.requestCount <= 0 {
		return 0
	}
	return float64(value.successCount) / float64(value.requestCount) * 100
}

func avgTps(value counters) float64 {
	if value.outputTokens <= 0 || value.generationMs <= 0 {
		return 0
	}
	return float64(value.outputTokens) / (float64(value.generationMs) / 1000)
}

func cacheHitRate(value counters) float64 {
	totalInput := value.inputTokens + value.cacheReadTokens + value.cacheWriteTokens
	if totalInput <= 0 {
		return 0
	}
	return float64(value.cacheReadTokens) / float64(totalInput) * 100
}

func roundMetric(value float64) float64 {
	return math.Round(value*100) / 100
}

func recordRedis(key bucketKey, sample Sample) {
	if !platformcache.RedisEnabled || platformcache.RDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	redisKey := redisBucketKey(key)
	pipe := platformcache.RDB.TxPipeline()
	pipe.HIncrBy(ctx, redisKey, "req", 1)
	if sample.Success {
		pipe.HIncrBy(ctx, redisKey, "ok", 1)
	}
	if sample.LatencyMs > 0 {
		pipe.HIncrBy(ctx, redisKey, "lat", sample.LatencyMs)
	}
	if sample.HasTtft && sample.TtftMs >= 0 {
		pipe.HIncrBy(ctx, redisKey, "ttft", sample.TtftMs)
		pipe.HIncrBy(ctx, redisKey, "ttft_n", 1)
	}
	if sample.OutputTokens > 0 && sample.GenerationMs > 0 {
		pipe.HIncrBy(ctx, redisKey, "out", sample.OutputTokens)
		pipe.HIncrBy(ctx, redisKey, "gen_ms", sample.GenerationMs)
	}
	writeCacheRedisCounters(ctx, pipe, redisKey, sample)
	pipe.Expire(ctx, redisKey, time.Hour)
	if sample.ChannelID > 0 {
		channelKey := channelRedisBucketKey(sample.ChannelID, key.bucketTs)
		pipe.HIncrBy(ctx, channelKey, "req", 1)
		if sample.Success {
			pipe.HIncrBy(ctx, channelKey, "ok", 1)
		}
		if sample.LatencyMs > 0 {
			pipe.HIncrBy(ctx, channelKey, "lat", sample.LatencyMs)
		}
		if sample.HasTtft && sample.TtftMs >= 0 {
			pipe.HIncrBy(ctx, channelKey, "ttft", sample.TtftMs)
			pipe.HIncrBy(ctx, channelKey, "ttft_n", 1)
		}
		if sample.OutputTokens > 0 && sample.GenerationMs > 0 {
			pipe.HIncrBy(ctx, channelKey, "out", sample.OutputTokens)
			pipe.HIncrBy(ctx, channelKey, "gen_ms", sample.GenerationMs)
		}
		writeCacheRedisCounters(ctx, pipe, channelKey, sample)
		pipe.Expire(ctx, channelKey, 2*time.Hour)
	}
	_, _ = pipe.Exec(ctx)
}

func writeCacheRedisCounters(ctx context.Context, pipe redis.Pipeliner, key string, sample Sample) {
	if sample.InputTokens > 0 {
		pipe.HIncrBy(ctx, key, "input", sample.InputTokens)
	}
	if sample.CacheReadTokens > 0 {
		pipe.HIncrBy(ctx, key, "cache_read", sample.CacheReadTokens)
	}
	if sample.CacheWriteTokens > 0 {
		pipe.HIncrBy(ctx, key, "cache_write", sample.CacheWriteTokens)
	}
}

func mergeRedisActiveBuckets(merged map[bucketKey]counters, params QueryParams, startTs int64, endTs int64) {
	if !platformcache.RedisEnabled || platformcache.RDB == nil || params.Model == "" || params.Group == "" {
		return
	}
	active := bucketStart(time.Now().Unix())
	if active < startTs || active > endTs {
		return
	}
	key := bucketKey{model: params.Model, group: params.Group, bucketTs: active}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	values, err := platformcache.RDB.HGetAll(ctx, redisBucketKey(key)).Result()
	if err != nil || len(values) == 0 {
		return
	}
	mergeCounters(merged, key, redisCounters(values))
}

func redisBucketKey(key bucketKey) string {
	return fmt.Sprintf("perf:%s:%s:%d", key.model, key.group, key.bucketTs)
}
