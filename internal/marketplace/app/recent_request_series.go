package app

import (
	"strings"
	"time"

	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

const (
	marketplaceRecentWindowHours    = 6
	marketplaceRecentWindowSegments = 12
	marketplaceRecentBucketSeconds  = int64(marketplaceRecentWindowHours * 3600 / marketplaceRecentWindowSegments)
)

func marketplaceRecentRequestSeries(groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel) (map[int][]RecentRequestBucket, error) {
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

	windowStart, windowEnd := marketplaceRecentWindow(time.Now().Unix())
	if platformdb.LogDB == nil {
		return buildMarketplaceRecentRequestSeries(windowStart, groupChannelIDs, nil), nil
	}
	rows, err := gatewaystore.LoadGroupModelRequestBuckets(
		windowStart,
		windowEnd,
		marketplaceRecentBucketSeconds,
		groupNames,
	)
	if err != nil {
		return nil, err
	}
	return buildMarketplaceRecentRequestSeries(windowStart, groupChannelIDs, rows), nil
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

func buildMarketplaceRecentRequestSeries(windowStart int64, groupChannelIDs map[string]int, rows []gatewaystore.GroupModelRequestBucket) map[int][]RecentRequestBucket {
	result := make(map[int][]RecentRequestBucket, len(groupChannelIDs))
	successCounts := make(map[int][]int64, len(groupChannelIDs))
	for _, channelID := range groupChannelIDs {
		if _, exists := result[channelID]; exists {
			continue
		}
		result[channelID] = newMarketplaceRecentRequestSeries(windowStart)
		successCounts[channelID] = make([]int64, marketplaceRecentWindowSegments)
	}

	for _, row := range rows {
		channelID, exists := groupChannelIDs[row.GroupName]
		if !exists || row.BucketIndex < 0 || row.BucketIndex >= marketplaceRecentWindowSegments {
			continue
		}
		index := int(row.BucketIndex)
		bucket := &result[channelID][index]
		bucket.RequestCount += row.RequestCount
		successCounts[channelID][index] += row.SuccessCount
	}
	for channelID, series := range result {
		for index := range series {
			if series[index].RequestCount > 0 {
				series[index].SuccessRate = round2(float64(successCounts[channelID][index]) / float64(series[index].RequestCount) * 100)
			}
		}
	}
	return result
}

func loadOfficialGroupRecentRequestStatuses(groupNames []string) map[string]string {
	statuses := buildRecentRequestStatusesByGroup(groupNames, nil)
	if len(groupNames) == 0 || platformdb.LogDB == nil {
		return statuses
	}

	windowStart, windowEnd := marketplaceRecentWindow(time.Now().Unix())
	rows, err := gatewaystore.LoadGroupModelRequestBuckets(
		windowStart,
		windowEnd,
		marketplaceRecentBucketSeconds,
		groupNames,
	)
	if err != nil {
		return statuses
	}
	return buildRecentRequestStatusesByGroup(groupNames, rows)
}

func buildRecentRequestStatusesByGroup(groupNames []string, rows []gatewaystore.GroupModelRequestBucket) map[string]string {
	statuses := make(map[string]string, len(groupNames))
	for _, groupName := range groupNames {
		statuses[groupName] = gatewaydomain.RequestHealthUnknown
	}
	type bucketCounts struct {
		requests int64
		success  int64
	}
	counts := make(map[string][]bucketCounts, len(groupNames))
	for _, row := range rows {
		if row.BucketIndex < 0 || row.BucketIndex >= marketplaceRecentWindowSegments {
			continue
		}
		if _, ok := counts[row.GroupName]; !ok {
			counts[row.GroupName] = make([]bucketCounts, marketplaceRecentWindowSegments)
		}
		bucket := &counts[row.GroupName][row.BucketIndex]
		bucket.requests += row.RequestCount
		bucket.success += row.SuccessCount
	}
	for groupName, buckets := range counts {
		for index := len(buckets) - 1; index >= 0; index-- {
			bucket := buckets[index]
			if bucket.requests <= 0 {
				continue
			}
			successRate := float64(bucket.success) / float64(bucket.requests) * 100
			statuses[groupName] = gatewaydomain.ClassifyRequestHealth(successRate, bucket.requests)
			break
		}
	}
	return statuses
}
