package app

import (
	"strings"
	"sync"
	"time"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

var recentSeriesCache struct {
	sync.Mutex
	at   time.Time
	data map[int][]RecentRequestBucket
	key  string
}

const (
	marketplaceRecentWindowHours    = 6
	marketplaceRecentWindowSegments = 6
	marketplaceRecentBucketSeconds  = int64(3600)
)

func marketplaceRecentRequestSeries(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel) (map[int][]RecentRequestBucket, error) {
	cacheKey := ""
	for _, group := range groups {
		cacheKey += group.InternalGroupName + ";"
	}
	recentSeriesCache.Lock()
	if time.Since(recentSeriesCache.at) < 10*time.Second && recentSeriesCache.data != nil && recentSeriesCache.key == cacheKey {
		data := recentSeriesCache.data
		recentSeriesCache.Unlock()
		return data, nil
	}
	recentSeriesCache.Unlock()

	groupChannelIDs := make(map[string]int, len(groups))
	groupNames := make([]string, 0, len(groups))
	for _, group := range groups {
		channel := channels[group.ChannelID]
		groupName := strings.TrimSpace(group.InternalGroupName)
		if channel.InternalChannelID == nil || *channel.InternalChannelID <= 0 || groupName == "" {
			continue
		}
		groupChannelIDs[groupName] = *channel.InternalChannelID
		groupNames = append(groupNames, groupName)
	}
	if len(groupNames) == 0 {
		return map[int][]RecentRequestBucket{}, nil
	}

	windowStart, _ := marketplaceRecentWindow(time.Now().Unix())
	if platformdb.DB == nil || platformdb.LogDB == nil {
		return buildMarketplaceRecentRequestSeries(windowStart, groupChannelIDs, nil), nil
	}
	rows, err := auditprojection.QuerySeriesByGroupModels(marketplaceRecentWindowHours, groupNames)
	if err != nil {
		return nil, err
	}
	data := buildMarketplaceRecentRequestSeries(windowStart, groupChannelIDs, rows)
	recentSeriesCache.Lock()
	recentSeriesCache.at, recentSeriesCache.data, recentSeriesCache.key = time.Now(), data, cacheKey
	recentSeriesCache.Unlock()
	return data, nil
}

func marketplaceRecentWindow(now int64) (int64, int64) {
	currentBucketStart := now - now%marketplaceRecentBucketSeconds
	windowStart := currentBucketStart - int64(marketplaceRecentWindowSegments-1)*marketplaceRecentBucketSeconds
	return windowStart, currentBucketStart + marketplaceRecentBucketSeconds
}

func emptyMarketplaceRecentRequestSeries(now int64) []RecentRequestBucket {
	windowStart, _ := marketplaceRecentWindow(now)
	return newMarketplaceRecentRequestSeries(windowStart)
}

func newMarketplaceRecentRequestSeries(windowStart int64) []RecentRequestBucket {
	series := make([]RecentRequestBucket, marketplaceRecentWindowSegments)
	for index := range series {
		series[index].Ts = windowStart + int64(index)*marketplaceRecentBucketSeconds
	}
	return series
}

// buildMarketplaceRecentRequestSeries combines every model in a group by
// request volume. It only consumes the compact perf_metrics projection and
// never falls back to scanning the raw audit log.
func buildMarketplaceRecentRequestSeries(windowStart int64, groupChannelIDs map[string]int, rows []auditprojection.GroupModelSeries) map[int][]RecentRequestBucket {
	result := make(map[int][]RecentRequestBucket, len(groupChannelIDs))
	weightedSuccess := make(map[int][]float64, len(groupChannelIDs))
	for _, channelID := range groupChannelIDs {
		if _, exists := result[channelID]; exists {
			continue
		}
		result[channelID] = newMarketplaceRecentRequestSeries(windowStart)
		weightedSuccess[channelID] = make([]float64, marketplaceRecentWindowSegments)
	}

	for _, row := range rows {
		channelID, exists := groupChannelIDs[row.Group]
		if !exists {
			continue
		}
		for _, point := range row.Series {
			bucketIndex := (point.Ts - windowStart) / marketplaceRecentBucketSeconds
			if point.Ts < windowStart || bucketIndex < 0 || bucketIndex >= marketplaceRecentWindowSegments || point.RequestCount <= 0 {
				continue
			}
			bucket := &result[channelID][bucketIndex]
			bucket.RequestCount += point.RequestCount
			weightedSuccess[channelID][bucketIndex] += point.SuccessRate * float64(point.RequestCount)
		}
	}
	for channelID, series := range result {
		for index := range series {
			if series[index].RequestCount > 0 {
				series[index].SuccessRate = round2(weightedSuccess[channelID][index] / float64(series[index].RequestCount))
			}
		}
	}
	return result
}

func loadOfficialGroupRecentRequestStatuses(groupNames []string) map[string]string {
	statuses := buildRecentRequestStatusesByGroup(groupNames, nil)
	if len(groupNames) == 0 || platformdb.DB == nil || platformdb.LogDB == nil {
		return statuses
	}
	rows, err := auditprojection.QuerySeriesByGroupModels(marketplaceRecentWindowHours, groupNames)
	if err != nil {
		return statuses
	}
	return buildRecentRequestStatusesByGroup(groupNames, rows)
}

func buildRecentRequestStatusesByGroup(groupNames []string, rows []auditprojection.GroupModelSeries) map[string]string {
	statuses := make(map[string]string, len(groupNames))
	for _, groupName := range groupNames {
		statuses[groupName] = gatewaydomain.RequestHealthUnknown
	}

	windowStart, _ := marketplaceRecentWindow(time.Now().Unix())
	type bucketCounts struct {
		requests        int64
		weightedSuccess float64
	}
	counts := make(map[string][]bucketCounts, len(groupNames))
	for _, row := range rows {
		if _, ok := counts[row.Group]; !ok {
			counts[row.Group] = make([]bucketCounts, marketplaceRecentWindowSegments)
		}
		for _, point := range row.Series {
			index := (point.Ts - windowStart) / marketplaceRecentBucketSeconds
			if point.Ts < windowStart || index < 0 || index >= marketplaceRecentWindowSegments || point.RequestCount <= 0 {
				continue
			}
			bucket := &counts[row.Group][index]
			bucket.requests += point.RequestCount
			bucket.weightedSuccess += point.SuccessRate * float64(point.RequestCount)
		}
	}
	for groupName, buckets := range counts {
		for index := len(buckets) - 1; index >= 0; index-- {
			bucket := buckets[index]
			if bucket.requests <= 0 {
				continue
			}
			statuses[groupName] = gatewaydomain.ClassifyRequestHealth(bucket.weightedSuccess/float64(bucket.requests), bucket.requests)
			break
		}
	}
	return statuses
}
