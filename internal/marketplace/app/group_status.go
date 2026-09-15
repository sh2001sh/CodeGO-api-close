package app

import (
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
)

// ListMarketplaceGroupStatus reads one complete status snapshot. It does not
// paginate or load marketplace feedback, personal prices and concurrency leases.
func ListMarketplaceGroupStatus(viewerUserID int) ([]GroupStatusListItem, error) {
	var groups []marketplaceschema.Group
	if err := publicGroupsQuery(GroupQuery{ViewerUserID: viewerUserID, IncludeAccess: viewerUserID > 0}).
		Where("source_type <> ?", marketplacedomain.SourceTypeOfficial).
		Order("updated_at DESC, id ASC").Find(&groups).Error; err != nil {
		return nil, err
	}
	channels, err := marketplaceChannelReadMap(groups)
	if err != nil {
		return nil, err
	}
	snapshots, err := rankingSnapshotsForList(groups, channels, 24)
	if err != nil {
		return nil, err
	}
	series, err := marketplaceRecentRequestSeries(groups, channels)
	if err != nil {
		return nil, err
	}
	items := make([]GroupListItem, 0, len(groups))
	for _, group := range groups {
		channel, exists := channels[group.ChannelID]
		if !exists {
			continue
		}
		channelID := 0
		if channel.InternalChannelID != nil {
			channelID = *channel.InternalChannelID
		}
		items = append(items, groupListItem(group, channel, decodeModels(channel.DeclaredModels), snapshots[group.ID], series[channelID]))
	}
	official, err := listOfficialGroupStatus(viewerUserID)
	if err != nil {
		return nil, err
	}
	items = append(items, official...)
	sortGroupItems(items, "score", "desc")
	result := make([]GroupStatusListItem, 0, len(items))
	for _, item := range items {
		result = append(result, compactGroupStatusItem(item))
	}
	return result, nil
}

const groupStatusWindowSegments = 6

func compactGroupStatusItem(item GroupListItem) GroupStatusListItem {
	return GroupStatusListItem{
		ID: item.ID, PublicSlug: item.PublicSlug, SystemDisplayName: item.SystemDisplayName,
		SourceType: item.SourceType, SourceLabel: item.SourceLabel, Models: item.Models,
		SuccessRate: item.SuccessRate, CacheHitRate: item.CacheHitRate,
		LatestRequestStatus:        item.LatestRequestStatus,
		RecentRequestSeries:        aggregateRecentRequestSeries(item.RecentRequestSeries, groupStatusWindowSegments),
		RecentRequestBucketSeconds: marketplaceRecentBucketSeconds * (marketplaceRecentWindowSegments / groupStatusWindowSegments),
		RequestCount:               item.RequestCount,
	}
}

func aggregateRecentRequestSeries(series []RecentRequestBucket, segments int) []RecentRequestBucket {
	if segments <= 0 || len(series) == 0 || len(series)%segments != 0 {
		return series
	}
	width := len(series) / segments
	result := make([]RecentRequestBucket, 0, segments)
	for start := 0; start < len(series); start += width {
		bucket := RecentRequestBucket{Ts: series[start].Ts}
		var weightedSuccess float64
		for _, source := range series[start : start+width] {
			bucket.RequestCount += source.RequestCount
			weightedSuccess += source.SuccessRate * float64(source.RequestCount)
		}
		if bucket.RequestCount > 0 {
			bucket.SuccessRate = round2(weightedSuccess / float64(bucket.RequestCount))
		}
		result = append(result, bucket)
	}
	return result
}
