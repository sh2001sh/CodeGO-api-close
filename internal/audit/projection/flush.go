package projection

import (
	"fmt"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"strconv"
	"time"
)

func flushLoop() {
	for {
		interval := getPerfMetricsFlushIntervalMinutes()
		time.Sleep(time.Duration(interval) * time.Minute)
		setting := getPerfMetricsSetting()
		if !setting.Enabled {
			continue
		}
		flushCompletedBuckets()
		cleanupExpiredMetrics(setting.RetentionDays)
	}
}

func flushCompletedBuckets() {
	currentBucket := bucketStart(time.Now().Unix())
	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if k.bucketTs >= currentBucket {
			return true
		}

		bucket := value.(*atomicBucket)
		drained := bucket.drain()
		if drained.requestCount == 0 {
			deleteOldEmptyBucket(k, key)
			return true
		}

		err := upsertMetric(&perfMetricRecord{
			ModelName:        k.model,
			Group:            k.group,
			BucketTs:         k.bucketTs,
			RequestCount:     drained.requestCount,
			SuccessCount:     drained.successCount,
			TotalLatencyMs:   drained.totalLatencyMs,
			TtftSumMs:        drained.ttftSumMs,
			TtftCount:        drained.ttftCount,
			OutputTokens:     drained.outputTokens,
			GenerationMs:     drained.generationMs,
			InputTokens:      drained.inputTokens,
			CacheReadTokens:  drained.cacheReadTokens,
			CacheWriteTokens: drained.cacheWriteTokens,
		})
		if err != nil {
			bucket.addCounters(drained)
			platformobservability.SysError(fmt.Sprintf("failed to flush perf metric bucket model=%s group=%s bucket=%d: %s", k.model, k.group, k.bucketTs, err.Error()))
			return true
		}

		deleteOldEmptyBucket(k, key)
		return true
	})
	flushCompletedChannelBuckets(currentBucket)
}

func deleteOldEmptyBucket(k bucketKey, rawKey any) {
	if k.bucketTs < bucketStart(time.Now().Add(-24*time.Hour).Unix()) {
		hotBuckets.Delete(rawKey)
	}
}

func cleanupExpiredMetrics(retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	if err := deletePerfMetricsBefore(cutoff); err != nil {
		platformobservability.SysError("failed to cleanup expired perf metrics: " + err.Error())
	}
	if err := deleteChannelPerfMetricsBefore(cutoff); err != nil {
		platformobservability.SysError("failed to cleanup expired channel perf metrics: " + err.Error())
	}
}

func redisCounters(values map[string]string) counters {
	result := counters{
		requestCount:     parseRedisInt(values["req"]),
		successCount:     parseRedisInt(values["ok"]),
		totalLatencyMs:   parseRedisInt(values["lat"]),
		ttftSumMs:        parseRedisInt(values["ttft"]),
		ttftCount:        parseRedisInt(values["ttft_n"]),
		outputTokens:     parseRedisInt(values["out"]),
		generationMs:     parseRedisInt(values["gen_ms"]),
		inputTokens:      parseRedisInt(values["input"]),
		cacheReadTokens:  parseRedisInt(values["cache_read"]),
		cacheWriteTokens: parseRedisInt(values["cache_write"]),
		attemptTtftSumMs: parseRedisInt(values["attempt_ttft_sum"]),
		attemptTtftCount: parseRedisInt(values["attempt_ttft_n"]),
		e2eTtftSumMs:     parseRedisInt(values["e2e_ttft_sum"]),
		e2eTtftCount:     parseRedisInt(values["e2e_ttft_n"]),
	}
	result.attemptTtftHistogram = make([]int64, metricHistogramBuckets)
	result.e2eTtftHistogram = make([]int64, metricHistogramBuckets)
	for index := 0; index < metricHistogramBuckets; index++ {
		result.attemptTtftHistogram[index] = parseRedisInt(values[fmt.Sprintf("attempt_ttft_h%d", index)])
		result.e2eTtftHistogram[index] = parseRedisInt(values[fmt.Sprintf("e2e_ttft_h%d", index)])
	}
	return result
}

func parseRedisInt(value string) int64 {
	if value == "" {
		return 0
	}
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}
