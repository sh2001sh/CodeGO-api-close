package app

import (
	"math"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
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
		metrics := loadOfficialGroupMetrics(groupName)
		items = append(items, AutoRoutePoolItem{
			GroupID: routeKey, SourceType: marketplacedomain.SourceTypeOfficial,
			SystemDisplayName: groupName, SourceLabel: description,
			LifecycleStatus: marketplacedomain.LifecycleActive,
			Multiplier: multiplier, Availability: metrics.Availability,
			SuccessRate: metrics.SuccessRate, CacheHitRate: metrics.CacheHitRate,
			AvgLatencyMS: metrics.AvgLatencyMS, RequestCount: metrics.RequestCount,
			MetricsAvailable: metrics.RequestCount > 0,
			LatestRequestStatus: metrics.Status,
			RouteScore:          round2(math.Max(multiplier, 0.000001)),
			Models:              models, Selected: isSelected, Priority: priority,
		})
	}
	return items
}

type officialGroupMetrics struct {
	Availability float64
	SuccessRate float64
	CacheHitRate float64
	AvgLatencyMS float64
	RequestCount int64
	Status string
}

func loadOfficialGroupMetrics(group string) officialGroupMetrics {
	base := officialGroupMetrics{Availability: 100, Status: marketplacedomain.LifecycleActive}
	channels, err := gatewaystore.LoadEnabledChannelsForGroup(group)
	if err != nil || len(channels) == 0 {
		return base
	}
	ids := make([]int, 0, len(channels))
	for _, channel := range channels {
		ids = append(ids, channel.Id)
	}
	summaries, err := auditprojection.QuerySummaryByChannels(6, ids)
	if err != nil {
		return base
	}
	var total float64
	var successWeighted, cacheWeighted, latencyWeighted float64
	var count int64
	for _, summary := range summaries {
		if summary.RequestCount <= 0 {
			continue
		}
		requests := float64(summary.RequestCount)
		total += requests
		successWeighted += summary.SuccessRate * requests
		cacheWeighted += summary.CacheHitRate * requests
		latencyWeighted += float64(summary.AvgLatencyMs) * requests
		count += summary.RequestCount
	}
	if count == 0 {
		return base
	}
	success := successWeighted / total
	status := "failed"
	if success >= 95 {
		status = "healthy"
	} else if success >= 80 {
		status = "unstable"
	}
	return officialGroupMetrics{
		Availability: success,
		SuccessRate: success,
		CacheHitRate: cacheWeighted / total,
		AvgLatencyMS: latencyWeighted / total,
		RequestCount: count,
		Status: status,
	}
}
