package projection

import (
	"sort"
	"time"
)

func QuerySummaryAll(hours int) (SummaryAllResult, error) {
	startTs, endTs := metricWindow(hours)
	rows, err := getPerfMetricsSummaryAll(startTs, endTs)
	if err != nil {
		return SummaryAllResult{}, err
	}
	totals := make(map[string]counters, len(rows))
	for _, row := range rows {
		totals[row.ModelName] = countersFromSummaryRow(row)
	}
	mergeHotModelTotals(totals, startTs, endTs)
	return SummaryAllResult{Models: buildModelSummaries(totals)}, nil
}

func metricWindow(hours int) (int64, int64) {
	if hours <= 0 {
		hours = 24
	}
	if hours > 24*30 {
		hours = 24 * 30
	}
	endTs := time.Now().Unix()
	return endTs - int64(hours)*3600, endTs
}

func countersFromSummaryRow(row perfMetricSummaryRow) counters {
	return counters{
		requestCount: row.RequestCount, successCount: row.SuccessCount,
		totalLatencyMs: row.TotalLatencyMs, ttftSumMs: row.TtftSumMs,
		ttftCount: row.TtftCount, outputTokens: row.OutputTokens,
		generationMs: row.GenerationMs, inputTokens: row.InputTokens,
		cacheReadTokens: row.CacheReadTokens, cacheWriteTokens: row.CacheWriteTokens,
	}
}

func mergeHotModelTotals(totals map[string]counters, startTs, endTs int64) {
	hotBuckets.Range(func(key, value any) bool {
		metricKey := key.(bucketKey)
		if metricKey.bucketTs < startTs || metricKey.bucketTs > endTs {
			return true
		}
		current := totals[metricKey.model]
		mergeCounterValue(&current, value.(*atomicBucket).snapshot())
		totals[metricKey.model] = current
		return true
	})
}

func buildModelSummaries(totals map[string]counters) []ModelSummary {
	models := make([]ModelSummary, 0, len(totals))
	for name, total := range totals {
		if total.requestCount == 0 {
			continue
		}
		models = append(models, ModelSummary{
			ModelName: name, AvgTtftMs: avg(total.ttftSumMs, total.ttftCount),
			AvgLatencyMs: avg(total.totalLatencyMs, total.requestCount), SuccessRate: roundMetric(successRate(total)),
			AvgTps: roundMetric(avgTps(total)), CacheHitRate: roundMetric(cacheHitRate(total)),
			InputTokens: total.inputTokens, CacheReadTokens: total.cacheReadTokens,
			CacheWriteTokens: total.cacheWriteTokens, RequestCount: total.requestCount,
		})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].RequestCount > models[j].RequestCount })
	return models
}
