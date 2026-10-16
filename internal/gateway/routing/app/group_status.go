package app

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"golang.org/x/sync/singleflight"
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
	GroupID      string                     `json:"group_id"`
	Group        string                     `json:"group"`
	DisplayName  string                     `json:"display_name"`
	SourceType   string                     `json:"source_type"`
	Status       string                     `json:"status"`
	RequestCount int64                      `json:"request_count"`
	SuccessRate  *float64                   `json:"success_rate"`
	CacheHitRate *float64                   `json:"cache_hit_rate"`
	Models       []UserGroupModelStatusItem `json:"models"`
}

type groupStatusBuildContext struct {
	groupSources      map[string]string
	groupDisplayNames map[string]string
	groupRouteIDs     map[string]string
	groupSummaries    map[string][]*GroupModelStatusSummary
	seriesByModel     map[string][]UserGroupStatusBucket
	groupCacheRates   map[string]*float64
	modelCacheRates   map[string]*float64
	sampleWindowHours float64
	seriesWindowHours float64
	bucketSeconds     int64
	emptySeries       []UserGroupStatusBucket
}

var groupStatusLoads singleflight.Group
var publicStatusGroupLoads singleflight.Group

func BuildUserGroupStatus(userID int, hasUser bool) ([]UserGroupStatusItem, error) {
	pricing := loadGatewayPricing()
	groupNames, err := resolveVisibleGroupStatusGroups(userID, hasUser, pricing)
	if err != nil {
		return nil, err
	}
	// Resolve permissions on every request before joining an in-flight read.
	// The payload contains no personal prices; only identical authorized group
	// sets share a build. No result survives the in-flight call, so subsequent
	// requests still read current health and group metadata.
	encodedGroups, _ := json.Marshal(groupNames)
	digest := sha256.Sum256(encodedGroups)
	key := fmt.Sprintf("%p:%p:%x", platformdb.DB, platformdb.LogDB, digest)
	value, err, _ := groupStatusLoads.Do(key, func() (any, error) {
		return buildGroupStatusForGroups(pricing, groupNames)
	})
	if err != nil {
		return nil, err
	}
	// Like pricing projections, this shared response is read-only. The HTTP
	// handler serializes it directly without mutating slices or rate pointers.
	return value.([]UserGroupStatusItem), nil
}

func buildGroupStatusForGroups(pricing []gatewaydomain.Pricing, groupNames []string) ([]UserGroupStatusItem, error) {
	const requestHealthWindowMinutes = 6 * 60
	const requestHealthSegmentCount = 12

	groupSources := make(map[string]string, len(groupNames))
	groupDisplayNames := make(map[string]string, len(groupNames))
	groupRouteIDs := make(map[string]string, len(groupNames))
	for _, name := range groupNames {
		groupSources[name] = marketplacedomain.SourceTypeOfficial
		groupDisplayNames[name] = name
		groupRouteIDs[name] = "official:" + name
	}
	var groups []marketplaceschema.Group
	if platformdb.DB != nil && len(groupNames) > 0 {
		if err := platformdb.DB.Select("id, internal_group_name, system_display_name, source_type, channel_id").
			Where("internal_group_name IN ?", groupNames).Find(&groups).Error; err != nil {
			return nil, err
		}
	}
	for _, group := range groups {
		name := group.InternalGroupName
		if display := strings.TrimSpace(group.SystemDisplayName); display != "" {
			groupDisplayNames[name] = display
		}
		if group.SourceType == marketplacedomain.SourceTypeMarketplaceUser {
			groupSources[name] = group.SourceType
			groupRouteIDs[name] = group.ID
		}
	}

	groupSummaries := buildPricingGroupModelSummaries(pricing, groupNames)
	if err := mergeMarketplaceGroupModelsFromRows(groupSummaries, groupNames, groups); err != nil {
		return nil, err
	}
	seriesByModel, seriesWindowHours, bucketSeconds := queryGroupModelRecentHealth(
		groupNames,
		requestHealthWindowMinutes,
		requestHealthSegmentCount,
	)
	groupCacheRates, modelCacheRates := queryGroupCacheHitRates(groupNames, 24)

	context := groupStatusBuildContext{
		groupSources: groupSources, groupDisplayNames: groupDisplayNames, groupRouteIDs: groupRouteIDs, groupSummaries: groupSummaries,
		seriesByModel: seriesByModel, groupCacheRates: groupCacheRates,
		modelCacheRates: modelCacheRates, sampleWindowHours: float64(bucketSeconds) / 3600,
		seriesWindowHours: seriesWindowHours, bucketSeconds: bucketSeconds,
		emptySeries: emptyStatusSeries(requestHealthWindowMinutes, requestHealthSegmentCount, bucketSeconds),
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
	for _, summary := range modelSummaries {
		item := buildUserGroupModelStatusItem(groupName, summary, context)
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
	groupStatus, groupRequestCount, successRate := summarizeGroupModelRequestHealth(modelItems)
	return UserGroupStatusItem{GroupID: context.groupRouteIDs[groupName], Group: groupName, DisplayName: context.groupDisplayNames[groupName], SourceType: context.groupSources[groupName], Status: groupStatus,
		RequestCount: groupRequestCount, SuccessRate: successRate, CacheHitRate: context.groupCacheRates[groupName], Models: modelItems}
}

func buildUserGroupModelStatusItem(groupName string, summary *GroupModelStatusSummary, context groupStatusBuildContext) UserGroupModelStatusItem {
	key := groupName + "::" + summary.Model
	series := context.seriesByModel[key]
	if len(series) == 0 {
		series = context.emptySeries
		if series == nil {
			series = emptyStatusSeries(6*60, 12, context.bucketSeconds)
		}
	}
	rate, requestCount := latestNonEmptyGroupStatusBucket(series)
	return UserGroupModelStatusItem{
		Model: summary.Model, Status: classifyGroupModelRequestHealth(rate, requestCount),
		SuccessRate: rate, SampleHours: context.sampleWindowHours, SeriesWindow: context.seriesWindowHours,
		BucketSeconds: context.bucketSeconds, RequestCount: requestCount, CacheHitRate: context.modelCacheRates[key], Series: series,
	}
}

func queryGroupCacheHitRates(groupNames []string, hours int) (map[string]*float64, map[string]*float64) {
	groupRates := make(map[string]*float64, len(groupNames))
	modelRates := make(map[string]*float64)
	if len(groupNames) == 0 {
		return groupRates, modelRates
	}
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

func resolveVisibleGroupStatusGroups(userID int, hasUser bool, pricing []gatewaydomain.Pricing) ([]string, error) {
	if !hasUser || userID <= 0 {
		groups := make(map[string]struct{})
		for _, groupName := range collectPricingGroups(pricing) {
			addGroupStatusName(groups, groupName)
		}
		addMarketplaceStatusGroups(groups)
		if len(groups) == 0 {
			return gatewaystore.ListGroupStatusGroups()
		}
		return sortedGroupStatusNames(groups), nil
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
	value, err, _ := publicStatusGroupLoads.Do(fmt.Sprintf("%p", platformdb.DB), func() (any, error) {
		var names []string
		err := platformdb.DB.Model(&marketplaceschema.Group{}).
			Where("visibility = ? AND lifecycle_status IN ?", marketplacedomain.VisibilityPublic, []string{
				marketplacedomain.LifecycleActive,
				marketplacedomain.LifecycleDegraded,
			}).Pluck("internal_group_name", &names).Error
		return names, err
	})
	if err != nil {
		return
	}
	for _, name := range value.([]string) {
		addGroupStatusName(groups, name)
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

func mergeMarketplaceGroupModelsFromRows(summaries map[string][]*GroupModelStatusSummary, groupNames []string, groups []marketplaceschema.Group) error {
	visible := make(map[string]struct{}, len(groupNames))
	for _, name := range groupNames {
		visible[name] = struct{}{}
	}
	channelIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		channelIDs = append(channelIDs, group.ChannelID)
	}
	if len(channelIDs) == 0 {
		return nil
	}
	var channels []marketplaceschema.Channel
	if err := platformdb.DB.Select("id, declared_models").Where("id IN ?", channelIDs).Find(&channels).Error; err != nil {
		return err
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
	return nil
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
