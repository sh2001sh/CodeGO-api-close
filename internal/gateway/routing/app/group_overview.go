package app

import (
	"math"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
)

type UserGroupOverviewItem struct {
	Group            string   `json:"group"`
	AvgLatencyMs     *float64 `json:"avg_latency_ms"`
	SuccessRate      *float64 `json:"success_rate"`
	AvgTps           *float64 `json:"avg_tps"`
	RequestCount     int64    `json:"request_count"`
	ActiveModelCount int      `json:"active_model_count"`
}

type groupOverviewTotals struct {
	requestCount     int64
	activeModelCount int
	latencyTotal     float64
	latencyWeight    int64
	tpsTotal         float64
	tpsWeight        int64
	successTotal     float64
	successWeight    int64
}

// BuildUserGroupOverview returns 24-hour-style metrics for every group visible
// to the current user, including configured groups that have no recent traffic.
func BuildUserGroupOverview(userID int, hasUser bool, hours int) ([]UserGroupOverviewItem, error) {
	if hours <= 0 {
		hours = 24
	}

	groupNames, err := resolveVisibleGroupStatusGroups(userID, hasUser, loadGatewayPricing())
	if err != nil {
		return nil, err
	}
	rows, err := auditprojection.QuerySummaryByGroupModels(hours, groupNames)
	if err != nil {
		return nil, err
	}

	return buildUserGroupOverview(groupNames, rows), nil
}

func buildUserGroupOverview(groupNames []string, rows []auditprojection.GroupModelSummary) []UserGroupOverviewItem {
	totals := make(map[string]*groupOverviewTotals, len(groupNames))
	for _, groupName := range groupNames {
		totals[groupName] = &groupOverviewTotals{}
	}

	for _, row := range rows {
		total, ok := totals[row.Group]
		if !ok || row.RequestCount <= 0 {
			continue
		}
		total.requestCount += row.RequestCount
		total.activeModelCount++
		total.successTotal += row.SuccessRate * float64(row.RequestCount)
		total.successWeight += row.RequestCount
		if row.AvgLatencyMs > 0 {
			total.latencyTotal += float64(row.AvgLatencyMs) * float64(row.RequestCount)
			total.latencyWeight += row.RequestCount
		}
		if row.AvgTps > 0 {
			total.tpsTotal += row.AvgTps * float64(row.RequestCount)
			total.tpsWeight += row.RequestCount
		}
	}

	result := make([]UserGroupOverviewItem, 0, len(groupNames))
	for _, groupName := range groupNames {
		total := totals[groupName]
		item := UserGroupOverviewItem{
			Group:            groupName,
			RequestCount:     total.requestCount,
			ActiveModelCount: total.activeModelCount,
		}
		item.AvgLatencyMs = roundedWeightedMetric(total.latencyTotal, total.latencyWeight)
		item.AvgTps = roundedWeightedMetric(total.tpsTotal, total.tpsWeight)
		item.SuccessRate = roundedWeightedMetric(total.successTotal, total.successWeight)
		result = append(result, item)
	}
	return result
}

func roundedWeightedMetric(total float64, weight int64) *float64 {
	if weight <= 0 {
		return nil
	}
	value := math.Round(total/float64(weight)*100) / 100
	return &value
}
