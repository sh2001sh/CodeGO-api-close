package projection

import "sort"

type modelGroupKey struct {
	group string
	model string
}

func QuerySummaryByGroups(hours int, groups []string) ([]GroupSummary, error) {
	startTs, endTs := metricWindow(hours)
	rows, err := getPerfMetricsSummaryByGroups(startTs, endTs, groups)
	if err != nil {
		return nil, err
	}
	totals := make(map[string]counters, len(rows))
	for _, row := range rows {
		totals[row.Group] = countersFromSummaryRow(row)
	}
	mergeHotGroupTotals(totals, groupSet(groups), startTs, endTs)
	return buildGroupSummaries(groups, totals), nil
}

func QuerySummaryByGroupModels(hours int, groups []string) ([]GroupModelSummary, error) {
	startTs, endTs := metricWindow(hours)
	rows, err := getPerfMetricsSummaryByGroupModels(startTs, endTs, groups)
	if err != nil {
		return nil, err
	}
	totals := make(map[modelGroupKey]counters, len(rows))
	for _, row := range rows {
		totals[modelGroupKey{group: row.Group, model: row.ModelName}] = countersFromSummaryRow(row)
	}
	mergeHotGroupModelTotals(totals, groupSet(groups), startTs, endTs)
	return buildGroupModelSummaries(totals), nil
}

func groupSet(groups []string) map[string]struct{} {
	result := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		result[group] = struct{}{}
	}
	return result
}

func groupAllowed(group string, allowed map[string]struct{}) bool {
	if len(allowed) == 0 {
		return true
	}
	_, ok := allowed[group]
	return ok
}

func mergeHotGroupTotals(totals map[string]counters, allowed map[string]struct{}, startTs, endTs int64) {
	hotBuckets.Range(func(key, value any) bool {
		metricKey := key.(bucketKey)
		if metricKey.bucketTs < startTs || metricKey.bucketTs > endTs || !groupAllowed(metricKey.group, allowed) {
			return true
		}
		current := totals[metricKey.group]
		mergeCounterValue(&current, value.(*atomicBucket).snapshot())
		totals[metricKey.group] = current
		return true
	})
}

func mergeHotGroupModelTotals(totals map[modelGroupKey]counters, allowed map[string]struct{}, startTs, endTs int64) {
	hotBuckets.Range(func(key, value any) bool {
		metricKey := key.(bucketKey)
		if metricKey.bucketTs < startTs || metricKey.bucketTs > endTs || !groupAllowed(metricKey.group, allowed) {
			return true
		}
		mapKey := modelGroupKey{group: metricKey.group, model: metricKey.model}
		current := totals[mapKey]
		mergeCounterValue(&current, value.(*atomicBucket).snapshot())
		totals[mapKey] = current
		return true
	})
}

func buildGroupSummaries(groups []string, totals map[string]counters) []GroupSummary {
	results := make([]GroupSummary, 0, len(totals))
	for _, group := range groups {
		total, ok := totals[group]
		if !ok || total.requestCount == 0 {
			continue
		}
		results = append(results, GroupSummary{Group: group, SuccessRate: roundMetric(successRate(total)),
			CacheHitRate: roundMetric(cacheHitRate(total)), RequestCount: total.requestCount})
	}
	return results
}

func buildGroupModelSummaries(totals map[modelGroupKey]counters) []GroupModelSummary {
	results := make([]GroupModelSummary, 0, len(totals))
	for key, total := range totals {
		if total.requestCount == 0 {
			continue
		}
		results = append(results, GroupModelSummary{
			Group: key.group, ModelName: key.model, AvgLatencyMs: avg(total.totalLatencyMs, total.requestCount),
			AvgTtftMs: avg(total.ttftSumMs, total.ttftCount), SuccessRate: roundMetric(successRate(total)),
			AvgTps: roundMetric(avgTps(total)), CacheHitRate: roundMetric(cacheHitRate(total)), RequestCount: total.requestCount,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Group != results[j].Group {
			return results[i].Group < results[j].Group
		}
		if results[i].RequestCount != results[j].RequestCount {
			return results[i].RequestCount > results[j].RequestCount
		}
		return results[i].ModelName < results[j].ModelName
	})
	return results
}
