package app

import (
	"errors"
	"math"
	"sort"
	"strings"

	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

const maxAutoRoutePoolMembers = 50
const officialAutoRoutePrefix = "official:"

// ListAutoRoutePool returns every eligible official and marketplace group and
// marks the groups currently selected by the user.
func ListAutoRoutePool(ownerUserID int) (*AutoRoutePoolView, error) {
	groups, channels, err := loadAutoRouteGroups(ownerUserID)
	if err != nil {
		return nil, err
	}
	snapshots, err := loadAutoRouteSnapshots(groups)
	if err != nil {
		return nil, err
	}
	recentSeries, err := marketplaceRecentRequestSeries(groups, channels)
	if err != nil {
		return nil, err
	}
	selected, err := loadAutoRoutePoolSelection(ownerUserID)
	if err != nil {
		return nil, err
	}

	items := make([]AutoRoutePoolItem, 0, len(groups))
	selectedCount := 0
	for _, group := range groups {
		channel := channels[group.ChannelID]
		priority, isSelected := selected[group.ID]
		if isSelected {
			selectedCount++
		}
		availability, score := autoRouteMetrics(group, snapshots[group.ID])
		snapshot := snapshots[group.ID]
		channelID := 0
		if channel.InternalChannelID != nil {
			channelID = *channel.InternalChannelID
		}
		items = append(items, AutoRoutePoolItem{
			GroupID: group.ID, SourceType: marketplacedomain.SourceTypeMarketplaceUser, PublicSlug: group.PublicSlug,
			SystemDisplayName:   marketplaceDisplayName(publicSourceLabel(channel), group.Multiplier, channel.ID),
			SourceLabel:         publicSourceLabel(channel),
			LifecycleStatus:     group.LifecycleStatus,
			Multiplier:          group.Multiplier,
			Availability:        round2(availability * 100),
			SuccessRate:         round2(snapshot.RawSuccessRate),
			CacheHitRate:        round2(snapshot.CacheHitRate),
			AvgLatencyMS:        round2(snapshot.AvgLatencyMs),
			LatestRequestStatus: latestRequestStatus(recentSeries[channelID]),
			MetricsAvailable:    snapshot.RequestCount > 0,
			RouteScore:          round2(score),
			Observing:           snapshots[group.ID].Observing,
			RequestCount:        snapshots[group.ID].RequestCount,
			Models:              decodeModels(channel.DeclaredModels),
			Selected:            isSelected,
			Priority:            priority,
		})
	}
	officialItems := loadOfficialAutoRouteItems(ownerUserID, selected)
	for _, item := range officialItems {
		if item.Selected {
			selectedCount++
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Selected != items[j].Selected {
			return items[i].Selected
		}
		if items[i].Selected && items[i].Priority != items[j].Priority {
			return items[i].Priority < items[j].Priority
		}
		if items[i].RouteScore != items[j].RouteScore {
			return items[i].RouteScore < items[j].RouteScore
		}
		return items[i].GroupID < items[j].GroupID
	})
	return &AutoRoutePoolView{
		TokenGroup: gatewayroutingapp.AutoGroupName, SelectedCount: selectedCount, Items: items,
	}, nil
}

// ReplaceAutoRoutePool atomically replaces the current user's selected groups.
func ReplaceAutoRoutePool(ownerUserID int, req AutoRoutePoolUpdateRequest) (*AutoRoutePoolView, error) {
	groupIDs := normalizeAutoRouteGroupIDs(req.GroupIDs)
	if len(groupIDs) > maxAutoRoutePoolMembers {
		return nil, errors.New("全局 Auto 路由池最多可添加 50 个分组")
	}
	groups, _, err := loadAutoRouteGroups(ownerUserID)
	if err != nil {
		return nil, err
	}
	eligible := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		eligible[group.ID] = struct{}{}
	}
	for _, item := range loadOfficialAutoRouteItems(ownerUserID, nil) {
		eligible[item.GroupID] = struct{}{}
	}
	for _, groupID := range groupIDs {
		if _, ok := eligible[groupID]; !ok {
			return nil, errors.New("路由池包含不可用或无权访问的分组")
		}
	}

	err = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("owner_user_id = ?", ownerUserID).Delete(&marketplaceschema.AutoRoutePoolMember{}).Error; err != nil {
			return err
		}
		for index, groupID := range groupIDs {
			member := marketplaceschema.AutoRoutePoolMember{
				OwnerUserID: ownerUserID,
				GroupID:     groupID,
				Priority:    index + 1,
			}
			if err := tx.Create(&member).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ListAutoRoutePool(ownerUserID)
}

// ResolveAutoRouteBindings returns model-compatible pool members in routing
// order. The distributor tries them in order and falls through when a group
// has no currently healthy channel.
func ResolveAutoRouteBindings(ownerUserID int, modelName string, multiplierLimit float64) ([]RoutingBinding, error) {
	if err := ValidateMultiplierLimitValue(multiplierLimit); err != nil {
		return nil, err
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil, errors.New("全局 Auto 路由需要模型名称")
	}
	selected, err := loadAutoRoutePoolSelection(ownerUserID)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, errors.New("Auto 路由池为空，请先添加官方或第三方分组")
	}
	groups, channels, err := loadAutoRouteGroups(ownerUserID)
	if err != nil {
		return nil, err
	}
	snapshots, err := loadAutoRouteSnapshots(groups)
	if err != nil {
		return nil, err
	}
	type scoredBinding struct {
		binding  RoutingBinding
		score    float64
		priority int
	}
	candidates := make([]scoredBinding, 0, len(selected))
	overLimitCount := 0
	for _, group := range groups {
		priority, ok := selected[group.ID]
		if !ok {
			continue
		}
		channel := channels[group.ChannelID]
		if !containsFold(decodeModels(channel.DeclaredModels), modelName) {
			continue
		}
		if !MultiplierWithinLimit(group.Multiplier, multiplierLimit) {
			overLimitCount++
			continue
		}
		_, score := autoRouteMetrics(group, snapshots[group.ID])
		candidates = append(candidates, scoredBinding{
			binding: RoutingBinding{
				RouteKey: group.ID,
				GroupID:  group.ID, InternalGroup: group.InternalGroupName,
				OwnerUserID: group.OwnerUserID, SourceType: group.SourceType,
				CreditPoolPolicy: group.CreditPoolPolicy, Multiplier: group.Multiplier,
				ModelPrices: decodeChannelModelPrices(channel.ModelPrices),
				Models:      decodeModels(channel.DeclaredModels),
			},
			score: score, priority: priority,
		})
	}
	for _, item := range loadOfficialAutoRouteItems(ownerUserID, selected) {
		priority, ok := selected[item.GroupID]
		if !ok || !containsFold(item.Models, modelName) {
			continue
		}
		if !MultiplierWithinLimit(item.Multiplier, multiplierLimit) {
			overLimitCount++
			continue
		}
		candidates = append(candidates, scoredBinding{
			binding: RoutingBinding{
				RouteKey: item.GroupID, InternalGroup: strings.TrimPrefix(item.GroupID, officialAutoRoutePrefix),
				SourceType:       marketplacedomain.SourceTypeOfficial,
				CreditPoolPolicy: marketplacedomain.CreditPolicyOfficialDefault,
				Multiplier:       item.Multiplier,
				Models:           item.Models,
			},
			score: item.RouteScore, priority: priority,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		if candidates[i].score != candidates[j].score {
			return candidates[i].score < candidates[j].score
		}
		return candidates[i].binding.GroupID < candidates[j].binding.GroupID
	})
	bindings := make([]RoutingBinding, 0, len(candidates))
	for _, candidate := range candidates {
		bindings = append(bindings, candidate.binding)
	}
	if len(bindings) == 0 {
		if overLimitCount > 0 {
			return nil, multiplierLimitExceededError(multiplierLimit)
		}
		return nil, errors.New("Auto 路由池没有支持该模型的可用分组")
	}
	return bindings, nil
}

func HasConfiguredAutoRoutePool(ownerUserID int) bool {
	var count int64
	if platformdb.DB.Model(&marketplaceschema.AutoRoutePoolMember{}).
		Where("owner_user_id = ?", ownerUserID).Count(&count).Error != nil {
		return false
	}
	return count > 0
}

// ListSelectedAutoRouteModels returns the deduplicated models exposed by the
// user's configured Auto route pool. The boolean distinguishes an empty pool
// from a user who has not configured the pool and should use legacy AutoGroups.
func ListSelectedAutoRouteModels(ownerUserID int) ([]string, bool, error) {
	selected, err := loadAutoRoutePoolSelection(ownerUserID)
	if err != nil {
		return nil, false, err
	}
	if len(selected) == 0 {
		return nil, false, nil
	}

	models := make(map[string]string)
	groups, channels, err := loadAutoRouteGroups(ownerUserID)
	if err != nil {
		return nil, true, err
	}
	for _, group := range groups {
		if _, ok := selected[group.ID]; !ok {
			continue
		}
		for _, model := range decodeModels(channels[group.ChannelID].DeclaredModels) {
			key := strings.ToLower(strings.TrimSpace(model))
			if key != "" {
				models[key] = strings.TrimSpace(model)
			}
		}
	}
	for _, item := range loadOfficialAutoRouteItems(ownerUserID, selected) {
		if !item.Selected {
			continue
		}
		for _, model := range item.Models {
			key := strings.ToLower(strings.TrimSpace(model))
			if key != "" {
				models[key] = strings.TrimSpace(model)
			}
		}
	}

	result := make([]string, 0, len(models))
	for _, model := range models {
		result = append(result, model)
	}
	sort.Strings(result)
	return result, true, nil
}

func loadAutoRouteGroups(ownerUserID int) ([]marketplaceschema.Group, map[string]marketplaceschema.Channel, error) {
	var groups []marketplaceschema.Group
	err := platformdb.DB.Where(
		"source_type = ? AND verification_status = ? AND lifecycle_status IN ? AND (visibility = ? OR owner_user_id = ?)",
		marketplacedomain.SourceTypeMarketplaceUser,
		marketplacedomain.VerificationPassed,
		[]string{marketplacedomain.LifecycleActive, marketplacedomain.LifecycleDegraded},
		marketplacedomain.VisibilityPublic,
		ownerUserID,
	).Limit(1000).Find(&groups).Error
	if err != nil {
		return nil, nil, err
	}
	channels, err := channelMap(groups)
	if err != nil {
		return nil, nil, err
	}
	filtered := groups[:0]
	for _, group := range groups {
		channel, ok := channels[group.ChannelID]
		if !ok || channel.InternalChannelID == nil || len(decodeModels(channel.DeclaredModels)) == 0 {
			continue
		}
		filtered = append(filtered, group)
	}
	return filtered, channels, nil
}

func loadAutoRoutePoolSelection(ownerUserID int) (map[string]int, error) {
	var members []marketplaceschema.AutoRoutePoolMember
	if err := platformdb.DB.Where("owner_user_id = ?", ownerUserID).Order("priority ASC, id ASC").Find(&members).Error; err != nil {
		return nil, err
	}
	selected := make(map[string]int, len(members))
	for index, member := range members {
		priority := member.Priority
		if priority <= 0 {
			priority = index + 1
		}
		selected[member.GroupID] = priority
	}
	return selected, nil
}

func loadAutoRouteSnapshots(groups []marketplaceschema.Group) (map[string]marketplaceschema.RankingSnapshot, error) {
	groupIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
	}
	result := make(map[string]marketplaceschema.RankingSnapshot, len(groupIDs))
	if len(groupIDs) == 0 {
		return result, nil
	}
	var snapshots []marketplaceschema.RankingSnapshot
	if err := platformdb.DB.Where(
		"group_id IN ? AND window_hours = ? AND ranking_version = ?", groupIDs, 24, rankingVersion,
	).Find(&snapshots).Error; err != nil {
		return nil, err
	}
	for _, snapshot := range snapshots {
		result[snapshot.GroupID] = snapshot
	}
	return result, nil
}

func normalizeAutoRouteGroupIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func autoRouteMetrics(group marketplaceschema.Group, snapshot marketplaceschema.RankingSnapshot) (float64, float64) {
	availability := snapshot.WilsonSuccessRate / 100
	if snapshot.RequestCount == 0 {
		availability = 0.85
	}
	if group.LifecycleStatus == marketplacedomain.LifecycleDegraded {
		availability *= 0.8
	}
	availability = math.Max(0.2, math.Min(availability, 1))
	multiplier := math.Max(group.Multiplier, 0.000001)
	return availability, multiplier / (availability * availability)
}
