package app

import (
	"errors"
	"math"
	"sort"
	"strings"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

const maxAutoRoutePoolMembers = 50

// ListAutoRoutePool returns every eligible third-party group and marks the
// groups currently selected by the user.
func ListAutoRoutePool(ownerUserID int) (*AutoRoutePoolView, error) {
	groups, channels, err := loadAutoRouteGroups(ownerUserID)
	if err != nil {
		return nil, err
	}
	snapshots, err := loadAutoRouteSnapshots(groups)
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
		_, isSelected := selected[group.ID]
		if isSelected {
			selectedCount++
		}
		availability, score := autoRouteMetrics(group, snapshots[group.ID])
		items = append(items, AutoRoutePoolItem{
			GroupID: group.ID, PublicSlug: group.PublicSlug,
			SystemDisplayName: group.SystemDisplayName,
			SourceLabel:       publicSourceLabel(channel),
			LifecycleStatus:   group.LifecycleStatus,
			Multiplier:        group.Multiplier,
			Availability:      round2(availability * 100),
			RouteScore:        round2(score),
			Observing:         snapshots[group.ID].Observing,
			RequestCount:      snapshots[group.ID].RequestCount,
			Models:            decodeModels(channel.DeclaredModels),
			Selected:          isSelected,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Selected != items[j].Selected {
			return items[i].Selected
		}
		if items[i].RouteScore != items[j].RouteScore {
			return items[i].RouteScore < items[j].RouteScore
		}
		return items[i].GroupID < items[j].GroupID
	})
	return &AutoRoutePoolView{
		TokenGroup: marketplacedomain.TokenAutoGroupValue, SelectedCount: selectedCount, Items: items,
	}, nil
}

// ReplaceAutoRoutePool atomically replaces the current user's selected groups.
func ReplaceAutoRoutePool(ownerUserID int, req AutoRoutePoolUpdateRequest) (*AutoRoutePoolView, error) {
	groupIDs := normalizeAutoRouteGroupIDs(req.GroupIDs)
	if len(groupIDs) > maxAutoRoutePoolMembers {
		return nil, errors.New("第三方 Auto 路由池最多可添加 50 个分组")
	}
	groups, _, err := loadAutoRouteGroups(ownerUserID)
	if err != nil {
		return nil, err
	}
	eligible := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		eligible[group.ID] = struct{}{}
	}
	for _, groupID := range groupIDs {
		if _, ok := eligible[groupID]; !ok {
			return nil, errors.New("路由池包含不可用或无权访问的第三方分组")
		}
	}

	err = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("owner_user_id = ?", ownerUserID).Delete(&marketplaceschema.AutoRoutePoolMember{}).Error; err != nil {
			return err
		}
		for _, groupID := range groupIDs {
			member := marketplaceschema.AutoRoutePoolMember{OwnerUserID: ownerUserID, GroupID: groupID}
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
func ResolveAutoRouteBindings(ownerUserID int, modelName string) ([]RoutingBinding, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil, errors.New("第三方 Auto 路由需要模型名称")
	}
	selected, err := loadAutoRoutePoolSelection(ownerUserID)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, errors.New("第三方 Auto 路由池为空，请先在模型广场添加分组")
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
		binding RoutingBinding
		score   float64
	}
	candidates := make([]scoredBinding, 0, len(selected))
	for _, group := range groups {
		if _, ok := selected[group.ID]; !ok {
			continue
		}
		channel := channels[group.ChannelID]
		if !containsFold(decodeModels(channel.DeclaredModels), modelName) {
			continue
		}
		_, score := autoRouteMetrics(group, snapshots[group.ID])
		candidates = append(candidates, scoredBinding{
			binding: RoutingBinding{
				GroupID: group.ID, InternalGroup: group.InternalGroupName,
				OwnerUserID: group.OwnerUserID, SourceType: group.SourceType,
				CreditPoolPolicy: group.CreditPoolPolicy, Multiplier: group.Multiplier,
			},
			score: score,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
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
		return nil, errors.New("第三方 Auto 路由池没有支持该模型的可用分组")
	}
	return bindings, nil
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

func loadAutoRoutePoolSelection(ownerUserID int) (map[string]struct{}, error) {
	var members []marketplaceschema.AutoRoutePoolMember
	if err := platformdb.DB.Where("owner_user_id = ?", ownerUserID).Find(&members).Error; err != nil {
		return nil, err
	}
	selected := make(map[string]struct{}, len(members))
	for _, member := range members {
		selected[member.GroupID] = struct{}{}
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
