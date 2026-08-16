package app

import (
	"encoding/json"
	"sort"
	"strings"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

type UserGroupStatusBucket struct {
	Ts           int64    `json:"ts"`
	SuccessRate  *float64 `json:"success_rate"`
	RequestCount int64    `json:"request_count"`
}

type UserGroupModelStatusItem struct {
	Model         string                  `json:"model"`
	Status        string                  `json:"status"`
	SuccessRate   *float64                `json:"success_rate"`
	SampleHours   float64                 `json:"sample_window"`
	SeriesWindow  float64                 `json:"series_window"`
	BucketSeconds int64                   `json:"bucket_seconds"`
	RequestCount  int64                   `json:"request_count"`
	CacheHitRate  *float64                `json:"cache_hit_rate"`
	Series        []UserGroupStatusBucket `json:"series"`
}

type UserGroupStatusItem struct {
	Group        string                     `json:"group"`
	SourceType   string                     `json:"source_type"`
	Status       string                     `json:"status"`
	RequestCount int64                      `json:"request_count"`
	CacheHitRate *float64                   `json:"cache_hit_rate"`
	Models       []UserGroupModelStatusItem `json:"models"`
}

type groupStatusBuildContext struct {
	groupSources      map[string]string
	groupSummaries    map[string][]*GroupModelStatusSummary
	successRates      map[string]*float64
	requestCounts     map[string]int64
	seriesByModel     map[string][]UserGroupStatusBucket
	groupCacheRates   map[string]*float64
	modelCacheRates   map[string]*float64
	sampleWindowHours float64
	seriesWindowHours float64
	bucketSeconds     int64
}

func BuildUserGroupStatus(userID int, hasUser bool) ([]UserGroupStatusItem, error) {
	const successSampleMinutes = 30
	const successSegmentCount = 1
	const timelineSampleMinutes = 24 * 60
	const timelineSegmentCount = 48

	pricing := loadGatewayPricing()
	groupNames, err := resolveVisibleGroupStatusGroups(userID, hasUser, pricing)
	if err != nil {
		return nil, err
	}
	groupSources := resolveGroupStatusSources(groupNames)

	groupSummaries := buildPricingGroupModelSummaries(pricing, groupNames)
	mergeMarketplaceGroupModels(groupSummaries, groupNames)
	successRates, _, requestCounts, sampleWindowHours, _ := queryGroupModelRecentHealth(groupNames, successSampleMinutes, successSegmentCount)
	_, seriesByModel, _, seriesWindowHours, bucketSeconds := queryGroupModelRecentHealth(groupNames, timelineSampleMinutes, timelineSegmentCount)
	groupCacheRates, modelCacheRates := queryGroupCacheHitRates(groupNames, 24)

	context := groupStatusBuildContext{
		groupSources: groupSources, groupSummaries: groupSummaries,
		successRates: successRates, requestCounts: requestCounts,
		seriesByModel: seriesByModel, groupCacheRates: groupCacheRates,
		modelCacheRates: modelCacheRates, sampleWindowHours: sampleWindowHours,
		seriesWindowHours: seriesWindowHours, bucketSeconds: bucketSeconds,
	}
	result := make([]UserGroupStatusItem, 0, len(groupNames))
	for _, groupName := range groupNames {
		result = append(result, buildUserGroupStatusItem(groupName, context))
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].RequestCount == result[j].RequestCount {
			return result[i].Group < result[j].Group
		}
		return result[i].RequestCount > result[j].RequestCount
	})

	return result, nil
}

func buildUserGroupStatusItem(groupName string, context groupStatusBuildContext) UserGroupStatusItem {
	modelSummaries := context.groupSummaries[groupName]
	modelItems := make([]UserGroupModelStatusItem, 0, len(modelSummaries))
	groupStatus := "unknown"
	groupRequestCount := int64(0)
	for _, summary := range modelSummaries {
		item := buildUserGroupModelStatusItem(groupName, summary, context)
		if groupStatus == "unknown" || modelStatusWeight(item.Status) < modelStatusWeight(groupStatus) {
			groupStatus = item.Status
		}
		groupRequestCount += item.RequestCount
		modelItems = append(modelItems, item)
	}
	sort.Slice(modelItems, func(i, j int) bool {
		if modelItems[i].RequestCount != modelItems[j].RequestCount {
			return modelItems[i].RequestCount > modelItems[j].RequestCount
		}
		left, right := modelStatusWeight(modelItems[i].Status), modelStatusWeight(modelItems[j].Status)
		if left != right {
			return left < right
		}
		return modelItems[i].Model < modelItems[j].Model
	})
	return UserGroupStatusItem{Group: groupName, SourceType: context.groupSources[groupName], Status: groupStatus,
		RequestCount: groupRequestCount, CacheHitRate: context.groupCacheRates[groupName], Models: modelItems}
}

func buildUserGroupModelStatusItem(groupName string, summary *GroupModelStatusSummary, context groupStatusBuildContext) UserGroupModelStatusItem {
	key := groupName + "::" + summary.Model
	requestCount := context.requestCounts[key]
	rate := context.successRates[key]
	series := context.seriesByModel[key]
	if len(series) == 0 {
		series = emptyStatusSeries(24*60, 48, context.bucketSeconds)
	}
	return UserGroupModelStatusItem{
		Model: summary.Model, Status: resolveGroupModelStatus(summary.Status, rate, requestCount),
		SuccessRate: rate, SampleHours: context.sampleWindowHours, SeriesWindow: context.seriesWindowHours,
		BucketSeconds: context.bucketSeconds, RequestCount: requestCount, CacheHitRate: context.modelCacheRates[key], Series: series,
	}
}

func queryGroupCacheHitRates(groupNames []string, hours int) (map[string]*float64, map[string]*float64) {
	groupRates := make(map[string]*float64, len(groupNames))
	modelRates := make(map[string]*float64)
	groupRows, err := auditprojection.QuerySummaryByGroups(hours, groupNames)
	if err == nil {
		for _, row := range groupRows {
			rate := row.CacheHitRate
			groupRates[row.Group] = &rate
		}
	}
	modelRows, err := auditprojection.QuerySummaryByGroupModels(hours, groupNames)
	if err == nil {
		for _, row := range modelRows {
			rate := row.CacheHitRate
			modelRates[row.Group+"::"+row.ModelName] = &rate
		}
	}
	return groupRates, modelRates
}

func resolveGroupStatusSources(groupNames []string) map[string]string {
	result := make(map[string]string, len(groupNames))
	for _, groupName := range groupNames {
		result[groupName] = marketplacedomain.SourceTypeOfficial
	}
	if platformdb.DB == nil || len(groupNames) == 0 {
		return result
	}
	var groups []marketplaceschema.Group
	if err := platformdb.DB.Select("internal_group_name").Where("internal_group_name IN ?", groupNames).Find(&groups).Error; err != nil {
		return result
	}
	for _, group := range groups {
		result[group.InternalGroupName] = marketplacedomain.SourceTypeMarketplaceUser
	}
	return result
}

func resolveVisibleGroupStatusGroups(userID int, hasUser bool, pricing []gatewaydomain.Pricing) ([]string, error) {
	if !hasUser || userID <= 0 {
		groups := collectPricingGroups(pricing)
		if len(groups) == 0 {
			return gatewaystore.ListGroupStatusGroups()
		}
		return groups, nil
	}

	userGroup, err := identitystore.LoadUserGroup(userID, false)
	if err != nil {
		return nil, err
	}

	groups := make(map[string]struct{})
	for groupName := range GetUserUsableGroups(userGroup) {
		if groupName == "auto" {
			for _, autoGroup := range GetUserAutoGroup(userGroup) {
				addGroupStatusName(groups, autoGroup)
			}
			continue
		}
		addGroupStatusName(groups, groupName)
	}
	addGroupStatusName(groups, userGroup)
	addMarketplaceStatusGroups(groups)

	if len(groups) == 0 {
		for _, groupName := range collectPricingGroups(pricing) {
			addGroupStatusName(groups, groupName)
		}
	}
	if len(groups) == 0 {
		return gatewaystore.ListGroupStatusGroups()
	}
	return sortedGroupStatusNames(groups), nil
}

func addMarketplaceStatusGroups(groups map[string]struct{}) {
	if platformdb.DB == nil {
		return
	}
	var items []marketplaceschema.Group
	if err := platformdb.DB.Select("internal_group_name").
		Where("visibility = ? AND lifecycle_status IN ?", marketplacedomain.VisibilityPublic, []string{
			marketplacedomain.LifecycleActive,
			marketplacedomain.LifecycleDegraded,
		}).Limit(1000).Find(&items).Error; err != nil {
		return
	}
	for _, item := range items {
		addGroupStatusName(groups, item.InternalGroupName)
	}
}

func collectPricingGroups(pricing []gatewaydomain.Pricing) []string {
	groups := make(map[string]struct{})
	for _, item := range pricing {
		for _, groupName := range item.EnableGroup {
			groupName = strings.TrimSpace(groupName)
			if groupName == "" || groupName == "auto" || groupName == "all" {
				continue
			}
			groups[groupName] = struct{}{}
		}
	}
	return sortedGroupStatusNames(groups)
}

func buildPricingGroupModelSummaries(pricing []gatewaydomain.Pricing, groupNames []string) map[string][]*GroupModelStatusSummary {
	if len(groupNames) == 0 {
		groupNames = collectPricingGroups(pricing)
	}
	summaries := make(map[string][]*GroupModelStatusSummary, len(groupNames))
	visibleGroups := make(map[string]struct{}, len(groupNames))
	knownModels := make(map[string]map[string]struct{}, len(groupNames))
	for _, groupName := range groupNames {
		summaries[groupName] = []*GroupModelStatusSummary{}
		visibleGroups[groupName] = struct{}{}
		knownModels[groupName] = make(map[string]struct{})
	}
	if len(groupNames) == 0 {
		return summaries
	}

	for _, item := range pricing {
		targetGroups := pricingTargetGroups(item.EnableGroup, groupNames, visibleGroups)
		if len(targetGroups) == 0 {
			continue
		}
		for _, groupName := range targetGroups {
			if _, ok := knownModels[groupName][item.ModelName]; ok {
				continue
			}
			knownModels[groupName][item.ModelName] = struct{}{}
			summaries[groupName] = append(summaries[groupName], &GroupModelStatusSummary{
				Group:           groupName,
				Model:           item.ModelName,
				Status:          "healthy",
				Channels:        0,
				EnabledChannels: 0,
			})
		}
	}

	return summaries
}

func mergeMarketplaceGroupModels(summaries map[string][]*GroupModelStatusSummary, groupNames []string) {
	if platformdb.DB == nil || len(groupNames) == 0 {
		return
	}
	visible := make(map[string]struct{}, len(groupNames))
	for _, groupName := range groupNames {
		visible[groupName] = struct{}{}
	}
	var groups []marketplaceschema.Group
	if err := platformdb.DB.Where("internal_group_name IN ?", groupNames).Find(&groups).Error; err != nil {
		return
	}
	channelIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		channelIDs = append(channelIDs, group.ChannelID)
	}
	if len(channelIDs) == 0 {
		return
	}
	var channels []marketplaceschema.Channel
	if err := platformdb.DB.Where("id IN ?", channelIDs).Find(&channels).Error; err != nil {
		return
	}
	modelsByChannel := make(map[string][]string, len(channels))
	for _, channel := range channels {
		var models []string
		if json.Unmarshal([]byte(channel.DeclaredModels), &models) == nil {
			modelsByChannel[channel.ID] = models
		}
	}
	for _, group := range groups {
		if _, ok := visible[group.InternalGroupName]; !ok {
			continue
		}
		known := make(map[string]struct{}, len(summaries[group.InternalGroupName]))
		for _, item := range summaries[group.InternalGroupName] {
			known[item.Model] = struct{}{}
		}
		for _, model := range modelsByChannel[group.ChannelID] {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, ok := known[model]; ok {
				continue
			}
			known[model] = struct{}{}
			summaries[group.InternalGroupName] = append(summaries[group.InternalGroupName], &GroupModelStatusSummary{
				Group: group.InternalGroupName, Model: model, Status: "healthy",
				Channels: 1, EnabledChannels: 1,
			})
		}
	}
}

func pricingTargetGroups(enableGroups []string, groupNames []string, visibleGroups map[string]struct{}) []string {
	targets := make([]string, 0, len(enableGroups))
	seen := make(map[string]struct{}, len(enableGroups))
	allGroups := len(groupNames) > 0
	for _, groupName := range enableGroups {
		groupName = strings.TrimSpace(groupName)
		if groupName == "" || groupName == "auto" {
			continue
		}
		if groupName == "all" {
			if allGroups {
				for _, visibleGroup := range groupNames {
					if _, ok := seen[visibleGroup]; ok {
						continue
					}
					if _, ok := visibleGroups[visibleGroup]; !ok {
						continue
					}
					seen[visibleGroup] = struct{}{}
					targets = append(targets, visibleGroup)
				}
			}
			continue
		}
		if _, ok := visibleGroups[groupName]; !ok {
			continue
		}
		if _, ok := seen[groupName]; ok {
			continue
		}
		seen[groupName] = struct{}{}
		targets = append(targets, groupName)
	}
	return targets
}
