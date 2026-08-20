package app

import (
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
)

func overlayLiveGroupModelLogHealth(
	seriesByModel map[string][]UserGroupStatusBucket,
	windowStart int64,
	windowEnd int64,
	bucketSeconds int64,
	groupNames []string,
) bool {
	alignedStart, alignedEnd, segmentCount := buildAlignedStatusWindow(windowEnd-1, windowEnd-windowStart, bucketSeconds)
	liveBucketStart := alignedEnd - bucketSeconds
	rows, err := gatewaystore.LoadGroupModelRequestBuckets(liveBucketStart, alignedEnd, bucketSeconds, groupNames)
	if err != nil {
		return false
	}
	applyLiveGroupModelLogRows(seriesByModel, rows, alignedStart, segmentCount, bucketSeconds, liveBucketStart)
	return len(rows) > 0
}

func applyLiveGroupModelLogRows(
	seriesByModel map[string][]UserGroupStatusBucket,
	rows []gatewaystore.GroupModelRequestBucket,
	windowStart int64,
	segmentCount int,
	bucketSeconds int64,
	liveBucketStart int64,
) {
	if segmentCount <= 0 || bucketSeconds <= 0 {
		return
	}
	bucketIndex := (liveBucketStart - windowStart) / bucketSeconds
	if bucketIndex < 0 || bucketIndex >= int64(segmentCount) {
		return
	}

	for _, row := range rows {
		if row.RequestCount <= 0 {
			continue
		}
		key := row.GroupName + "::" + row.ModelName
		if len(seriesByModel[key]) != segmentCount {
			seriesByModel[key] = buildStatusSeries(windowStart, segmentCount, bucketSeconds)
		}
		rate := float64(row.SuccessCount) / float64(row.RequestCount) * 100
		seriesByModel[key][bucketIndex] = UserGroupStatusBucket{
			Ts:           liveBucketStart,
			SuccessRate:  &rate,
			RequestCount: row.RequestCount,
		}
	}
}
