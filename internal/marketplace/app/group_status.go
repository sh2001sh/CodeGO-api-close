package app

import (
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
)

// ListMarketplaceGroupStatus reads one complete status snapshot. It does not
// paginate or load marketplace feedback, personal prices and concurrency leases.
func ListMarketplaceGroupStatus(viewerUserID int) ([]GroupListItem, error) {
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
	return items, nil
}
