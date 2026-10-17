package projection

import "sort"

// QueryRankingSummary only reads explicitly public group/model pairs. Counts
// come from the relay projection once, never from channel or billing totals.
func QueryRankingSummary(hours int, scope map[string][]string) (SummaryAllResult, error) {
	groups, allowed := rankingMetricScope(scope, "")
	if len(groups) == 0 {
		return SummaryAllResult{Models: []ModelSummary{}}, nil
	}
	startTs, endTs := metricWindow(hours)
	rows, err := getPerfMetricsSummaryByGroupModels(startTs, endTs, groups)
	if err != nil {
		return SummaryAllResult{}, err
	}
	byGroupModel := make(map[modelGroupKey]counters, len(rows))
	for _, row := range rows {
		byGroupModel[modelGroupKey{group: row.Group, model: row.ModelName}] = countersFromSummaryRow(row)
	}
	mergeHotGroupModelTotals(byGroupModel, groupSet(groups), startTs, endTs)
	totals := make(map[string]counters)
	for key, value := range byGroupModel {
		if _, public := allowed[key]; !public {
			continue
		}
		total := totals[key.model]
		mergeCounterValue(&total, value)
		totals[key.model] = total
	}
	return SummaryAllResult{Models: buildModelSummaries(totals)}, nil
}

func QueryRankingTrend(model string, hours int, scope map[string][]string) (QueryResult, error) {
	groups, allowed := rankingMetricScope(scope, model)
	merged := make(map[bucketKey]counters)
	if model == "" || len(groups) == 0 {
		return buildQueryResult(model, merged), nil
	}
	startTs, endTs := metricWindow(hours)
	rows, err := getPerfMetricsForGroups(model, groups, startTs, endTs)
	if err != nil {
		return QueryResult{}, err
	}
	for _, row := range rows {
		mergeCounters(merged, bucketKey{model: row.ModelName, group: row.Group, bucketTs: row.BucketTs}, countersFromRecord(row))
	}
	hotBuckets.Range(func(key, value any) bool {
		metricKey := key.(bucketKey)
		if metricKey.model != model || metricKey.bucketTs < startTs || metricKey.bucketTs > endTs {
			return true
		}
		if _, public := allowed[modelGroupKey{group: metricKey.group, model: metricKey.model}]; public {
			mergeCounters(merged, metricKey, value.(*atomicBucket).snapshot())
		}
		return true
	})
	return buildQueryResult(model, merged), nil
}

func rankingMetricScope(scope map[string][]string, model string) ([]string, map[modelGroupKey]struct{}) {
	groups := make([]string, 0, len(scope))
	allowed := make(map[modelGroupKey]struct{})
	for group, models := range scope {
		if group == "" {
			continue
		}
		matched := false
		for _, name := range models {
			if name == "" || (model != "" && name != model) {
				continue
			}
			allowed[modelGroupKey{group: group, model: name}] = struct{}{}
			matched = true
		}
		if matched {
			groups = append(groups, group)
		}
	}
	sort.Strings(groups)
	return groups, allowed
}
