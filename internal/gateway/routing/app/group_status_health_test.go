package app

import (
	"testing"

	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
	"github.com/stretchr/testify/require"
)

func TestLatestNonEmptyGroupStatusBucketUsesSharedThresholds(t *testing.T) {
	t.Parallel()

	failedRate := 74.99
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

	healthy := 90.01
	unstable := 75.0
	failed := 74.99
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

func TestSummarizeGroupModelRequestHealthUsesRequestWeightedGroupRate(t *testing.T) {
	t.Parallel()

	healthyRate := 100.0
	failedRate := 50.0
	status, requests, successRate := summarizeGroupModelRequestHealth([]UserGroupModelStatusItem{
		{Model: "healthy", Status: gatewaydomain.RequestHealthHealthy, SuccessRate: &healthyRate, RequestCount: 3},
		{Model: "failed", Status: gatewaydomain.RequestHealthFailed, SuccessRate: &failedRate, RequestCount: 1},
	})

	require.Equal(t, gatewaydomain.RequestHealthUnstable, status)
	require.EqualValues(t, 4, requests)
	require.NotNil(t, successRate)
	require.Equal(t, 87.5, *successRate)
}

func TestSummarizeGroupModelRequestHealthDoesNotLetRareFailureOverrideHealthyTraffic(t *testing.T) {
	t.Parallel()

	healthyRate := 99.0
	failedRate := 50.0
	status, requests, successRate := summarizeGroupModelRequestHealth([]UserGroupModelStatusItem{
		{Model: "healthy", Status: gatewaydomain.RequestHealthHealthy, SuccessRate: &healthyRate, RequestCount: 100},
		{Model: "failed", Status: gatewaydomain.RequestHealthFailed, SuccessRate: &failedRate, RequestCount: 1},
	})

	require.Equal(t, gatewaydomain.RequestHealthHealthy, status)
	require.EqualValues(t, 101, requests)
	require.NotNil(t, successRate)
	require.InDelta(t, 98.51, *successRate, 0.01)
}
