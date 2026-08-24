package app

import (
	"math"
	"testing"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
)

func TestBuildUserGroupOverviewWeightsMetricsAndKeepsEmptyGroups(t *testing.T) {
	items := buildUserGroupOverview(
		[]string{"default", "premium", "unused"},
		[]auditprojection.GroupModelSummary{
			{Group: "default", ModelName: "a", AvgLatencyMs: 100, SuccessRate: 100, AvgTps: 20, RequestCount: 90},
			{Group: "default", ModelName: "b", AvgLatencyMs: 1000, SuccessRate: 50, AvgTps: 2, RequestCount: 10},
			{Group: "premium", ModelName: "c", AvgLatencyMs: 80, SuccessRate: 99, AvgTps: 30, RequestCount: 5},
			{Group: "hidden", ModelName: "d", AvgLatencyMs: 1, SuccessRate: 100, AvgTps: 50, RequestCount: 1},
		},
	)

	if len(items) != 3 {
		t.Fatalf("expected 3 visible groups, got %d", len(items))
	}
	if items[0].RequestCount != 100 || items[0].ActiveModelCount != 2 {
		t.Fatalf("unexpected default totals: %+v", items[0])
	}
	assertMetric(t, "latency", items[0].AvgLatencyMs, 190)
	assertMetric(t, "tps", items[0].AvgTps, 18.2)
	assertMetric(t, "success", items[0].SuccessRate, 95)
	if items[2].RequestCount != 0 || items[2].AvgLatencyMs != nil || items[2].SuccessRate != nil {
		t.Fatalf("expected empty group without metrics, got %+v", items[2])
	}
}

func assertMetric(t *testing.T, name string, actual *float64, expected float64) {
	t.Helper()
	if actual == nil || math.Abs(*actual-expected) > 0.001 {
		t.Fatalf("unexpected %s: got %v, want %.2f", name, actual, expected)
	}
}
