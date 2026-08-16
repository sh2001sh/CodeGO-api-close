package projection

import "sort"

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
	mergeHotGroupModelBuckets(buckets, groupSet(groups), startTs, endTs)
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

func mergeHotGroupModelBuckets(buckets map[modelGroupKey]map[int64]counters, allowed map[string]struct{}, startTs, endTs int64) {
	hotBuckets.Range(func(key, value any) bool {
		metricKey := key.(bucketKey)
		if metricKey.bucketTs < startTs || metricKey.bucketTs > endTs || !groupAllowed(metricKey.group, allowed) {
			return true
		}
		appendGroupModelBucket(buckets, modelGroupKey{group: metricKey.group, model: metricKey.model}, metricKey.bucketTs, value.(*atomicBucket).snapshot())
		return true
	})
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
