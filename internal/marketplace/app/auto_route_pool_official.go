package app

import (
	"math"

	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
)

func loadOfficialAutoRouteItems(ownerUserID int, selected map[string]int) []AutoRoutePoolItem {
	userGroup, err := identitystore.LoadUserGroup(ownerUserID, false)
	if err != nil {
		return []AutoRoutePoolItem{}
	}
	usable := gatewayroutingapp.GetUserUsableGroups(userGroup)
	items := make([]AutoRoutePoolItem, 0, len(usable))
	for groupName, description := range usable {
		if groupName == gatewayroutingapp.AutoGroupName {
			continue
		}
		models := gatewayroutingapp.EnabledModelsForGroup(groupName)
		if len(models) == 0 {
			continue
		}
		routeKey := officialAutoRoutePrefix + groupName
		priority, isSelected := selected[routeKey]
		multiplier := gatewayroutingapp.GetUserGroupRatio(userGroup, groupName)
		items = append(items, AutoRoutePoolItem{
			GroupID: routeKey, SourceType: marketplacedomain.SourceTypeOfficial,
			SystemDisplayName: groupName, SourceLabel: description,
			LifecycleStatus: marketplacedomain.LifecycleActive,
			Multiplier:      multiplier, Availability: 100,
			LatestRequestStatus: marketplacedomain.LifecycleActive,
			RouteScore:          round2(math.Max(multiplier, 0.000001)),
			Models:              models, Selected: isSelected, Priority: priority,
		})
	}
	return items
}
