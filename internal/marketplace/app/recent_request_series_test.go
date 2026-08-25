package app

import (
	"testing"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestBuildMarketplaceRecentRequestSeriesFillsEmptyBuckets(t *testing.T) {
	t.Parallel()

	seriesByChannel := buildMarketplaceRecentRequestSeries(1_000, map[string]int{
		"market-group": 42,
	}, []gatewaystore.GroupModelRequestBucket{
		{GroupName: "market-group", ModelName: "model-a", BucketIndex: 1, RequestCount: 3, SuccessCount: 3},
		{GroupName: "market-group", ModelName: "model-b", BucketIndex: 1, RequestCount: 1, SuccessCount: 0},
		{GroupName: "market-group", ModelName: "model-a", BucketIndex: 11, RequestCount: 2, SuccessCount: 1},
	})

	series := seriesByChannel[42]
	require.Len(t, series, marketplaceRecentWindowSegments)
	require.Equal(t, int64(1_000), series[0].Ts)
	require.Zero(t, series[0].RequestCount)
	require.Zero(t, series[0].SuccessRate)
	require.Equal(t, int64(4), series[1].RequestCount)
	require.Equal(t, 75.0, series[1].SuccessRate)
	require.Equal(t, int64(2), series[11].RequestCount)
	require.Equal(t, 50.0, series[11].SuccessRate)
	require.Equal(t, int64(1_000)+11*marketplaceRecentBucketSeconds, series[11].Ts)
}

func TestMarketplaceRecentRequestSeriesAggregatesAuditLogsIntoFixedWindow(t *testing.T) {
	_, logDB := openOwnerUsageLogTestDB(t)
	channelID := 71
	groupName := "market-fixed-window"
	now := time.Now().Unix()
	currentBucketStart := now - now%marketplaceRecentBucketSeconds
	previousBucketStart := currentBucketStart - marketplaceRecentBucketSeconds
	require.NoError(t, logDB.Create(&[]auditschema.Log{
		{CreatedAt: previousBucketStart + 10, Type: auditschema.LogTypeConsume, Group: groupName, ModelName: "model-a"},
		{CreatedAt: previousBucketStart + 20, Type: auditschema.LogTypeError, Group: groupName, ModelName: "model-a"},
		{CreatedAt: previousBucketStart + 25, Type: auditschema.LogTypeError, Group: groupName, ModelName: "model-a", Other: `{"error_code":"sensitive_words_detected"}`},
		{CreatedAt: previousBucketStart + 30, Type: auditschema.LogTypeError, Group: groupName, ModelName: "model-a", Other: `{"counted_in_success_rate":false}`},
		{CreatedAt: currentBucketStart + 10, Type: auditschema.LogTypeConsume, Group: groupName, ModelName: "model-b"},
	}).Error)

	seriesByChannel, err := marketplaceRecentRequestSeries(
		[]marketplaceschema.Group{{ID: "group-fixed", ChannelID: "channel-fixed", InternalGroupName: groupName}},
		map[string]marketplaceschema.Channel{
			"channel-fixed": {ID: "channel-fixed", InternalChannelID: &channelID},
		},
	)
	require.NoError(t, err)

	series := seriesByChannel[channelID]
	require.Len(t, series, marketplaceRecentWindowSegments)
	previous := recentRequestBucketAt(t, series, previousBucketStart)
	require.Equal(t, int64(2), previous.RequestCount)
	require.Equal(t, 50.0, previous.SuccessRate)
	current := recentRequestBucketAt(t, series, currentBucketStart)
	require.Equal(t, int64(1), current.RequestCount)
	require.Equal(t, 100.0, current.SuccessRate)
	require.Len(t, filterNonEmptyRecentRequestBuckets(series), 2)
}

func TestBuildRecentRequestStatusesByGroupUsesLatestNonEmptyBucket(t *testing.T) {
	t.Parallel()

	statuses := buildRecentRequestStatusesByGroup([]string{"stable", "idle"}, []gatewaystore.GroupModelRequestBucket{
		{GroupName: "stable", BucketIndex: 8, RequestCount: 5, SuccessCount: 2},
		{GroupName: "stable", BucketIndex: 10, RequestCount: 5, SuccessCount: 5},
	})

	require.Equal(t, "healthy", statuses["stable"])
	require.Equal(t, "unknown", statuses["idle"])
}

func TestBuildRecentRequestStatusesByGroupUsesSharedThresholds(t *testing.T) {
	t.Parallel()

	statuses := buildRecentRequestStatusesByGroup([]string{"unstable", "failed"}, []gatewaystore.GroupModelRequestBucket{
		{GroupName: "unstable", BucketIndex: 11, RequestCount: 20, SuccessCount: 17},
		{GroupName: "failed", BucketIndex: 11, RequestCount: 20, SuccessCount: 16},
	})

	require.Equal(t, "unstable", statuses["unstable"])
	require.Equal(t, "failed", statuses["failed"])
}

func recentRequestBucketAt(t *testing.T, series []RecentRequestBucket, ts int64) RecentRequestBucket {
	t.Helper()
	for _, bucket := range series {
		if bucket.Ts == ts {
			return bucket
		}
	}
	require.FailNow(t, "request bucket not found", "timestamp: %d", ts)
	return RecentRequestBucket{}
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
