package app

import (
	"math"
	"sort"
	"strings"
	"time"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
)

func queryGroupModelRecentHealth(groupNames []string, sampleMinutes int, segmentCount int) (map[string]*float64, map[string][]UserGroupStatusBucket, map[string]int64, float64, int64) {
	rates := make(map[string]*float64)
	seriesByModel := make(map[string][]UserGroupStatusBucket)
	requestCounts := make(map[string]int64)
	if len(groupNames) == 0 {
		return rates, seriesByModel, requestCounts, 0, 60
	}
	if segmentCount <= 0 {
		segmentCount = 20
	}

	windowSeconds := int64(sampleMinutes * 60)
	requestedBucketSeconds := windowSeconds / int64(segmentCount)
	if windowSeconds%int64(segmentCount) != 0 {
		requestedBucketSeconds++
	}
	if requestedBucketSeconds < 60 {
		requestedBucketSeconds = 60
	}

	sampleWindowHours := float64(windowSeconds) / 3600
	now := time.Now().Unix()
	windowStart, windowEnd, alignedSegments := buildAlignedStatusWindow(now, windowSeconds, requestedBucketSeconds)

	actualBucketSeconds, _ := fillGroupModelPerfHealth(
		rates,
		seriesByModel,
		requestCounts,
		windowStart,
		windowEnd,
		requestedBucketSeconds,
		alignedSegments,
		groupNames,
	)
	return rates, seriesByModel, requestCounts, sampleWindowHours, actualBucketSeconds
}

func fillGroupModelPerfHealth(
	rates map[string]*float64,
	seriesByModel map[string][]UserGroupStatusBucket,
	requestCounts map[string]int64,
	windowStart int64,
	windowEnd int64,
	bucketSeconds int64,
	segmentCount int,
	groupNames []string,
) (int64, bool) {
	actualBucketSeconds := auditprojection.PerfMetricsBucketSeconds()
	if actualBucketSeconds <= 0 {
		actualBucketSeconds = bucketSeconds
	}
	hours := int(math.Ceil(float64(windowEnd-windowStart) / 3600))
	if hours <= 0 {
		hours = 1
	}

	summaryRows, err := auditprojection.QuerySummaryByGroupModels(hours, groupNames)
	if err != nil {
		return actualBucketSeconds, false
	}
	seriesRows, err := auditprojection.QuerySeriesByGroupModels(hours, groupNames)
	if err != nil && len(summaryRows) == 0 {
		return actualBucketSeconds, false
	}
	if len(summaryRows) == 0 && len(seriesRows) == 0 {
		return actualBucketSeconds, false
	}

	applyPerfSummaryRows(rates, requestCounts, summaryRows)

	applyPerfSeriesRows(seriesByModel, seriesRows, windowStart, windowEnd, actualBucketSeconds)
	return actualBucketSeconds, len(requestCounts) > 0 || len(seriesByModel) > 0
}

func applyPerfSummaryRows(rates map[string]*float64, requestCounts map[string]int64, rows []auditprojection.GroupModelSummary) {
	for _, row := range rows {
		if row.RequestCount <= 0 {
			continue
		}
		key := row.Group + "::" + row.ModelName
		requestCounts[key] += row.RequestCount
		rate := row.SuccessRate
		rates[key] = &rate
	}
}

func applyPerfSeriesRows(seriesByModel map[string][]UserGroupStatusBucket, rows []auditprojection.GroupModelSeries, windowStart, windowEnd, bucketSeconds int64) {
	alignedStart, _, alignedSegments := buildAlignedStatusWindow(windowEnd-1, windowEnd-windowStart, bucketSeconds)
	for _, row := range rows {
		key := row.Group + "::" + row.ModelName
		if _, ok := seriesByModel[key]; !ok {
			seriesByModel[key] = buildStatusSeries(alignedStart, alignedSegments, bucketSeconds)
		}
		for _, point := range row.Series {
			bucketIndex := (point.Ts - alignedStart) / bucketSeconds
			if bucketIndex < 0 || bucketIndex >= int64(alignedSegments) {
				continue
			}
			bucket := &seriesByModel[key][bucketIndex]
			bucket.RequestCount += point.RequestCount
			if point.RequestCount > 0 {
				rate := point.SuccessRate
				bucket.SuccessRate = &rate
			}
		}
	}
}

func modelStatusWeight(status string) int {
	switch status {
	case gatewaydomain.RequestHealthFailed:
		return 0
	case gatewaydomain.RequestHealthUnstable:
		return 1
	case gatewaydomain.RequestHealthUnknown:
		return 2
	default:
		return 3
	}
}

func classifyGroupModelRequestHealth(successRate *float64, requestCount int64) string {
	if requestCount <= 0 || successRate == nil {
		return gatewaydomain.RequestHealthUnknown
	}
	return gatewaydomain.ClassifyRequestHealth(*successRate, requestCount)
}

func summarizeGroupModelRequestHealth(items []UserGroupModelStatusItem) (string, int64, *float64) {
	requestCount := int64(0)
	weightedSuccess := float64(0)
	weightedRequests := int64(0)
	for _, item := range items {
		requestCount += item.RequestCount
		if item.RequestCount <= 0 || item.SuccessRate == nil {
			continue
		}
		weightedSuccess += *item.SuccessRate * float64(item.RequestCount)
		weightedRequests += item.RequestCount
	}
	if weightedRequests == 0 {
		return gatewaydomain.RequestHealthUnknown, requestCount, nil
	}
	rate := weightedSuccess / float64(weightedRequests)
	return gatewaydomain.ClassifyRequestHealth(rate, weightedRequests), requestCount, &rate
}

func latestNonEmptyGroupStatusBucket(series []UserGroupStatusBucket) (*float64, int64) {
	for index := len(series) - 1; index >= 0; index-- {
		bucket := series[index]
		if bucket.RequestCount <= 0 || bucket.SuccessRate == nil {
			continue
		}
		rate := *bucket.SuccessRate
		return &rate, bucket.RequestCount
	}
	return nil, 0
}

func buildStatusSeries(windowStart int64, segmentCount int, bucketSeconds int64) []UserGroupStatusBucket {
	series := make([]UserGroupStatusBucket, 0, segmentCount)
	for index := 0; index < segmentCount; index++ {
		series = append(series, UserGroupStatusBucket{
			Ts:           windowStart + int64(index)*bucketSeconds,
			SuccessRate:  nil,
			RequestCount: 0,
		})
	}
	return series
}

func buildAlignedStatusWindow(now int64, windowSeconds int64, bucketSeconds int64) (int64, int64, int) {
	if bucketSeconds <= 0 {
		bucketSeconds = 60
	}
	if windowSeconds <= 0 {
		windowSeconds = bucketSeconds
	}
	segmentCount := int((windowSeconds + bucketSeconds - 1) / bucketSeconds)
	if segmentCount <= 0 {
		segmentCount = 1
	}
	currentBucketStart := now - (now % bucketSeconds)
	windowEnd := currentBucketStart + bucketSeconds
	windowStart := windowEnd - int64(segmentCount)*bucketSeconds
	return windowStart, windowEnd, segmentCount
}

func emptyStatusSeries(sampleMinutes int, segmentCount int, bucketSeconds int64) []UserGroupStatusBucket {
	windowStart, _, alignedSegments := buildAlignedStatusWindow(time.Now().Unix(), int64(sampleMinutes*60), bucketSeconds)
	return buildStatusSeries(windowStart, alignedSegments, bucketSeconds)
}

func addGroupStatusName(groups map[string]struct{}, groupName string) {
	groupName = strings.TrimSpace(groupName)
	if groupName == "" || groupName == "auto" {
		return
	}
	groups[groupName] = struct{}{}
}

func sortedGroupStatusNames(groups map[string]struct{}) []string {
	result := make([]string, 0, len(groups))
	for groupName := range groups {
		result = append(result, groupName)
	}
	sort.Strings(result)
	return result
}
