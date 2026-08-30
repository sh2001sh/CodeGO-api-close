package app

import (
	"testing"

	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	"github.com/stretchr/testify/require"
)

func TestLatestNonEmptyGroupStatusBucketUsesSharedThresholds(t *testing.T) {
	t.Parallel()

	failedRate := 84.99
	healthyRate := 100.0
	rate, requests := latestNonEmptyGroupStatusBucket([]UserGroupStatusBucket{
		{SuccessRate: &failedRate, RequestCount: 4},
		{SuccessRate: &healthyRate, RequestCount: 20},
		{},
	})

	require.NotNil(t, rate)
	require.Equal(t, 100.0, *rate)
	require.EqualValues(t, 20, requests)
	require.Equal(t, gatewaydomain.RequestHealthHealthy, classifyGroupModelRequestHealth(rate, requests))
}

func TestClassifyGroupModelRequestHealthMatchesMarketplaceContract(t *testing.T) {
	t.Parallel()

	healthy := 90.0
	unstable := 85.0
	failed := 84.99
	require.Equal(t, gatewaydomain.RequestHealthUnknown, classifyGroupModelRequestHealth(nil, 0))
	require.Equal(t, gatewaydomain.RequestHealthHealthy, classifyGroupModelRequestHealth(&healthy, 1))
	require.Equal(t, gatewaydomain.RequestHealthUnstable, classifyGroupModelRequestHealth(&unstable, 1))
	require.Equal(t, gatewaydomain.RequestHealthFailed, classifyGroupModelRequestHealth(&failed, 1))
}

func TestSummarizeGroupModelRequestHealthIgnoresModelsWithoutRequests(t *testing.T) {
	t.Parallel()

	healthyRate := 100.0
	status, requests, successRate := summarizeGroupModelRequestHealth([]UserGroupModelStatusItem{
		{Model: "active", Status: gatewaydomain.RequestHealthHealthy, SuccessRate: &healthyRate, RequestCount: 4},
		{Model: "idle", Status: gatewaydomain.RequestHealthUnknown},
	})

	require.Equal(t, gatewaydomain.RequestHealthHealthy, status)
	require.EqualValues(t, 4, requests)
	require.NotNil(t, successRate)
	require.Equal(t, 100.0, *successRate)
}

func TestSummarizeGroupModelRequestHealthUsesWorstSampledModel(t *testing.T) {
	t.Parallel()

	healthyRate := 100.0
	failedRate := 50.0
	status, requests, successRate := summarizeGroupModelRequestHealth([]UserGroupModelStatusItem{
		{Model: "healthy", Status: gatewaydomain.RequestHealthHealthy, SuccessRate: &healthyRate, RequestCount: 3},
		{Model: "failed", Status: gatewaydomain.RequestHealthFailed, SuccessRate: &failedRate, RequestCount: 1},
	})

	require.Equal(t, gatewaydomain.RequestHealthFailed, status)
	require.EqualValues(t, 4, requests)
	require.NotNil(t, successRate)
	require.Equal(t, 87.5, *successRate)
}

func TestApplyLiveGroupModelLogRowsFillsLatestBucket(t *testing.T) {
	t.Parallel()

	const (
		windowStart     = int64(1_000)
		bucketSeconds   = int64(100)
		segmentCount    = 4
		liveBucketStart = int64(1_300)
	)
	previousRate := 100.0
	series := map[string][]UserGroupStatusBucket{
		"plus::gpt": {
			{Ts: 1_000, SuccessRate: &previousRate, RequestCount: 2},
			{Ts: 1_100},
			{Ts: 1_200},
			{Ts: 1_300},
		},
	}

	applyLiveGroupModelLogRows(series, []gatewaystore.GroupModelRequestBucket{{
		GroupName: "plus", ModelName: "gpt", RequestCount: 5, SuccessCount: 4,
	}}, windowStart, segmentCount, bucketSeconds, liveBucketStart)

	require.EqualValues(t, 2, series["plus::gpt"][0].RequestCount)
	require.EqualValues(t, 5, series["plus::gpt"][3].RequestCount)
	require.NotNil(t, series["plus::gpt"][3].SuccessRate)
	require.Equal(t, 80.0, *series["plus::gpt"][3].SuccessRate)
}

func TestApplyLiveGroupModelLogRowsBuildsSeriesForNewModel(t *testing.T) {
	t.Parallel()

	series := map[string][]UserGroupStatusBucket{}
	applyLiveGroupModelLogRows(series, []gatewaystore.GroupModelRequestBucket{{
		GroupName: "plus", ModelName: "new-model", RequestCount: 1, SuccessCount: 1,
	}}, 1_000, 4, 100, 1_300)

	require.Len(t, series["plus::new-model"], 4)
	require.Zero(t, series["plus::new-model"][2].RequestCount)
	require.EqualValues(t, 1, series["plus::new-model"][3].RequestCount)
	require.Equal(t, 100.0, *series["plus::new-model"][3].SuccessRate)
}
