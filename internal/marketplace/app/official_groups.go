package app

import (
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
)

// ListOfficialGroups returns the official groups visible to the current viewer.
// It is intentionally independent from the third-party marketplace ranking.
func ListOfficialGroups(viewerUserID int) (*OfficialGroupListView, error) {
	userGroup := ""
	if viewerUserID > 0 {
		var err error
		userGroup, err = identitystore.LoadUserGroup(viewerUserID, false)
		if err != nil {
			return nil, err
		}
	}
	items := loadOfficialAutoRouteItemsForUserGroup(userGroup, nil)
	result := make([]OfficialGroupListItem, 0, len(items))
	for _, item := range items {
		policy := gatewaystore.GetSubscriptionGroupPolicy(item.SystemDisplayName)
		result = append(result, OfficialGroupListItem{
			GroupID: item.GroupID, SystemDisplayName: item.SystemDisplayName,
			Description: item.SourceLabel, Multiplier: item.Multiplier,
			SubscriptionEnabled: policy.Enabled, SubscriptionMultiplier: policy.Multiplier,
			Models: item.Models, SuccessRate: item.SuccessRate, AvgLatencyMS: item.AvgLatencyMS,
			LatestRequestStatus: item.LatestRequestStatus, MetricsAvailable: item.MetricsAvailable,
			RequestCount: item.RequestCount,
		})
	}
	return &OfficialGroupListView{Items: result, Total: len(result)}, nil
}
