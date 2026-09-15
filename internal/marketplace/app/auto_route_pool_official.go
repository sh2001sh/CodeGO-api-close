package app

import (
	"math"
	"sort"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

func loadOfficialAutoRouteItems(ownerUserID int, selected map[string]int) []AutoRoutePoolItem {
	return loadOfficialAutoRouteItemsFiltered(ownerUserID, selected, false, true)
}

func loadOfficialAutoRouteItemsForSelection(ownerUserID int, selected map[string]int) []AutoRoutePoolItem {
	return loadOfficialAutoRouteItemsFiltered(ownerUserID, selected, true, true)
}

// loadOfficialAutoRouteItemsSummary is used by list endpoints that only need
// the declared model names. Loading audit summaries for every official group
// makes a simple route-pool list pay for one channel query plus one aggregate
// query per group.
func loadOfficialAutoRouteItemsSummary(ownerUserID int) []AutoRoutePoolItem {
	return loadOfficialAutoRouteItemsFiltered(ownerUserID, nil, false, false)
}

// loadOfficialAutoRouteModels returns only the model names exposed by the
// selected official groups. Model-list requests must not load audit metrics.
func loadOfficialAutoRouteModels(ownerUserID int, selected map[string]int) []string {
	items := loadOfficialAutoRouteItemsFiltered(ownerUserID, selected, true, false)
	models := make([]string, 0)
	for _, item := range items {
		models = append(models, item.Models...)
	}
	return models
}

func loadOfficialAutoRouteItemsFiltered(ownerUserID int, selected map[string]int, selectedOnly bool, includeMetrics bool) []AutoRoutePoolItem {
	userGroup, err := identitystore.LoadUserGroup(ownerUserID, false)
	if err != nil {
		return []AutoRoutePoolItem{}
	}
	usable := gatewayroutingapp.GetUserUsableGroups(userGroup)
	groupNames := make([]string, 0, len(usable))
	for groupName := range usable {
		if groupName != gatewayroutingapp.AutoGroupName {
			// During request routing, only selected official groups can be
			// candidates. Avoid loading request buckets and audit summaries for
			// every globally usable group on every request.
			if selectedOnly && selected != nil {
				if _, ok := selected[officialAutoRoutePrefix+groupName]; !ok {
					continue
				}
			}
			groupNames = append(groupNames, groupName)
		}
	}
	sort.Strings(groupNames)
	capabilities, err := loadOfficialGroupCapabilities(groupNames)
	if err != nil {
		platformobservability.SysError("load official route pool capabilities: " + err.Error())
		return nil
	}
	var recentStatuses map[string]string
	metricsByChannel := make(map[int]auditprojection.ChannelSummary)
	walletStats := make(map[string]channelConsumerStats)
	if includeMetrics {
		recentStatuses = loadOfficialGroupRecentRequestStatuses(groupNames)
		channelIDs := make([]int, 0)
		seen := make(map[int]bool)
		for _, capability := range capabilities {
			for _, id := range capability.ChannelIDs {
				if !seen[id] {
					channelIDs = append(channelIDs, id)
					seen[id] = true
				}
			}
		}
		summaries, err := auditprojection.QuerySummaryByChannels(6, channelIDs)
		if err != nil {
			platformobservability.SysError("load official route pool metrics: " + err.Error())
		}
		for _, summary := range summaries {
			metricsByChannel[summary.ChannelID] = summary
		}
		walletStats, err = officialWalletConsumerStats(groupNames, 24)
		if err != nil {
			platformobservability.SysError("load official route pool wallet costs: " + err.Error())
			walletStats = make(map[string]channelConsumerStats)
		}
	}
	items := make([]AutoRoutePoolItem, 0, len(usable))
	for _, groupName := range groupNames {
		description := usable[groupName]
		capability := capabilities[groupName]
		models := capability.Models
		if len(models) == 0 {
			continue
		}
		routeKey := officialAutoRoutePrefix + groupName
		priority, isSelected := selected[routeKey]
		multiplier := gatewayroutingapp.GetUserGroupRatio(userGroup, groupName)
		metrics := officialGroupMetrics{Availability: 100, Status: recentStatuses[groupName]}
		consumerStats := walletStats[groupName]
		if includeMetrics {
			metrics = aggregateOfficialGroupMetrics(capability.ChannelIDs, metricsByChannel, recentStatuses[groupName])
		}
		items = append(items, AutoRoutePoolItem{
			GroupID: routeKey, SourceType: marketplacedomain.SourceTypeOfficial,
			SystemDisplayName: groupName, SourceLabel: description,
			LifecycleStatus: marketplacedomain.LifecycleActive,
			Multiplier:      multiplier, Availability: metrics.Availability,
			SuccessRate: metrics.SuccessRate, CacheHitRate: metrics.CacheHitRate,
			AvgLatencyMS: metrics.AvgLatencyMS, RequestCount: metrics.RequestCount,
			MetricsAvailable:    metrics.RequestCount > 0,
			LatestRequestStatus: metrics.Status,
			RouteScore:          round2(math.Max(multiplier, 0.000001)),
			Models:              models, AvgConsumerAmount: consumerStats.averageConsumerAmount(), AvgConsumerAmountByModel: consumerStats.averageConsumerAmountsByModel(), Selected: isSelected, Priority: priority,
			MultiplierCardSupported: capability.MultiplierCard, MultiplierCardUserEnabled: capability.MultiplierCard,
		})
	}
	return items
}

type officialGroupMetrics struct {
	Availability float64
	SuccessRate  float64
	CacheHitRate float64
	AvgLatencyMS float64
	RequestCount int64
	Status       string
}

func aggregateOfficialGroupMetrics(ids []int, summaries map[int]auditprojection.ChannelSummary, recentStatus string) officialGroupMetrics {
	base := officialGroupMetrics{Availability: 100, Status: recentStatus}
	var total float64
	var successWeighted, cacheWeighted, latencyWeighted float64
	var count int64
	for _, id := range ids {
		summary := summaries[id]
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
	return officialGroupMetrics{
		Availability: success,
		SuccessRate:  success,
		CacheHitRate: cacheWeighted / total,
		AvgLatencyMS: latencyWeighted / total,
		RequestCount: count,
		Status:       recentStatus,
	}
}
