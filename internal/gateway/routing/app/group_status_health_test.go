package app

import (
	"testing"

	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	"github.com/stretchr/testify/require"
)

func TestShouldPreferLogHealthOnlyForShortWindows(t *testing.T) {
	t.Parallel()

	require.True(t, shouldPreferLogHealth(30*60, 30*60))
	require.False(t, shouldPreferLogHealth(24*60*60, 30*60))
	require.False(t, shouldPreferLogHealth(30*60, 60*60))
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
