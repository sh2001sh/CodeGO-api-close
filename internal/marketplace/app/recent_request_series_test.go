package app

import (
	"testing"
	"time"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	"github.com/stretchr/testify/require"
)

func TestBuildMarketplaceRecentRequestSeriesFillsHourlyBucketsAndWeightsModels(t *testing.T) {
	t.Parallel()

	const start = int64(3_600)
	seriesByChannel := buildMarketplaceRecentRequestSeries(start, map[string]int{
		"market-group": 42,
	}, []auditprojection.GroupModelSeries{
		{Group: "market-group", ModelName: "model-a", Series: []auditprojection.BucketPoint{
			{Ts: start + marketplaceRecentBucketSeconds, RequestCount: 3, SuccessRate: 100},
			{Ts: start + 5*marketplaceRecentBucketSeconds, RequestCount: 2, SuccessRate: 50},
		}},
		{Group: "market-group", ModelName: "model-b", Series: []auditprojection.BucketPoint{
			{Ts: start + marketplaceRecentBucketSeconds, RequestCount: 1, SuccessRate: 0},
		}},
	})

	series := seriesByChannel[42]
	require.Len(t, series, marketplaceRecentWindowSegments)
	require.Equal(t, start, series[0].Ts)
	require.Zero(t, series[0].RequestCount)
	require.EqualValues(t, 4, series[1].RequestCount)
	require.Equal(t, 75.0, series[1].SuccessRate)
	require.EqualValues(t, 2, series[5].RequestCount)
	require.Equal(t, 50.0, series[5].SuccessRate)
}

func TestBuildMarketplaceRecentRequestSeriesIgnoresOutsideWindow(t *testing.T) {
	t.Parallel()

	const start = int64(36_000)
	series := buildMarketplaceRecentRequestSeries(start, map[string]int{"group": 7}, []auditprojection.GroupModelSeries{{
		Group: "group", ModelName: "model", Series: []auditprojection.BucketPoint{
			{Ts: start - marketplaceRecentBucketSeconds, RequestCount: 10, SuccessRate: 0},
			{Ts: start + marketplaceRecentWindowSegments*marketplaceRecentBucketSeconds, RequestCount: 10, SuccessRate: 0},
		},
	}})[7]

	require.Empty(t, filterNonEmptyRecentRequestBuckets(series))
}

func TestBuildRecentRequestStatusesByGroupUsesLatestWeightedBucket(t *testing.T) {
	t.Parallel()

	start, _ := marketplaceRecentWindow(time.Now().Unix())
	rows := []auditprojection.GroupModelSeries{
		{Group: "stable", ModelName: "busy", Series: []auditprojection.BucketPoint{
			{Ts: start + 4*marketplaceRecentBucketSeconds, RequestCount: 100, SuccessRate: 99},
		}},
		{Group: "stable", ModelName: "rare-failure", Series: []auditprojection.BucketPoint{
			{Ts: start + 4*marketplaceRecentBucketSeconds, RequestCount: 1, SuccessRate: 0},
		}},
	}

	statuses := buildRecentRequestStatusesByGroup([]string{"stable", "idle"}, rows)

	require.Equal(t, "healthy", statuses["stable"])
	require.Equal(t, "unknown", statuses["idle"])
}

func TestBuildRecentRequestStatusesByGroupUsesSharedThresholds(t *testing.T) {
	t.Parallel()

	start, _ := marketplaceRecentWindow(time.Now().Unix())
	statuses := buildRecentRequestStatusesByGroup([]string{"unstable", "failed"}, []auditprojection.GroupModelSeries{
		{Group: "unstable", ModelName: "model", Series: []auditprojection.BucketPoint{{Ts: start, RequestCount: 20, SuccessRate: 85}}},
		{Group: "failed", ModelName: "model", Series: []auditprojection.BucketPoint{{Ts: start, RequestCount: 20, SuccessRate: 70}}},
	})

	require.Equal(t, "unstable", statuses["unstable"])
	require.Equal(t, "failed", statuses["failed"])
}

func filterNonEmptyRecentRequestBuckets(series []RecentRequestBucket) []RecentRequestBucket {
	result := make([]RecentRequestBucket, 0, len(series))
	for _, bucket := range series {
		if bucket.RequestCount > 0 {
			result = append(result, bucket)
		}
	}
	return result
}

func TestMarketplaceRecentWindowIsSixHourlyBuckets(t *testing.T) {
	start, end := marketplaceRecentWindow(1788740123)
	require.Equal(t, int64(6*3600), end-start)
	require.Equal(t, int64(3600), marketplaceRecentBucketSeconds)
	require.Equal(t, int64(0), start%3600)
	require.Len(t, newMarketplaceRecentRequestSeries(start), 6)
}
