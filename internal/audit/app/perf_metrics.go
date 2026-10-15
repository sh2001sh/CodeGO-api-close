package app

import (
	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
)

func BuildPerfMetricsSummary(hours int, group string) (any, error) {
	if hours <= 0 {
		hours = 24
	}
	if group == "" {
		return auditprojection.QuerySummaryAll(hours)
	}
	rows, err := auditprojection.QuerySummaryByGroupModels(hours, []string{group})
	if err != nil {
		return auditprojection.SummaryAllResult{}, err
	}
	models := make([]auditprojection.ModelSummary, 0, len(rows))
	for _, row := range rows {
		models = append(models, auditprojection.ModelSummary{
			ModelName:    row.ModelName,
			AvgTtftMs:    row.AvgTtftMs,
			AvgLatencyMs: row.AvgLatencyMs,
			SuccessRate:  row.SuccessRate,
			AvgTps:       row.AvgTps,
			CacheHitRate: row.CacheHitRate,
			RequestCount: row.RequestCount,
		})
	}
	return auditprojection.SummaryAllResult{Models: models}, nil
}

func BuildPerfMetrics(modelName string, group string, hours int) (*auditprojection.QueryResult, error) {
	if hours <= 0 {
		hours = 24
	}

	result, err := auditprojection.Query(auditprojection.QueryParams{
		Model: modelName,
		Group: group,
		Hours: hours,
	})
	if err != nil {
		return nil, err
	}

	result.Groups = filterActivePerfMetricGroups(result.Groups)
	return &result, nil
}

func filterActivePerfMetricGroups(groups []auditprojection.GroupResult) []auditprojection.GroupResult {
	activeGroups := gatewaystore.GetGroupRatioCopy()
	filtered := make([]auditprojection.GroupResult, 0, len(groups))
	for _, groupItem := range groups {
		if _, ok := activeGroups[groupItem.Group]; ok || groupItem.Group == "auto" {
			filtered = append(filtered, groupItem)
		}
	}
	return filtered
}
