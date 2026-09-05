package app

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

const maxAutoRoutePoolMembers = 10
const officialAutoRoutePrefix = "official:"

var ErrAutoRouteModelUnavailable = errors.New("Auto 路由池没有支持该模型的可用分组")

// ListAutoRoutePool returns every eligible official and marketplace group and
// marks the groups currently selected by the user.
func ListAutoRoutePool(ownerUserID int) (*AutoRoutePoolView, error) {
	groups, channels, err := loadAutoRouteGroups(ownerUserID)
	if err != nil {
		return nil, err
	}
	var snapshots map[string]marketplaceschema.RankingSnapshot
	var recentSeries map[int][]RecentRequestBucket
	loadSnapshots := func() error {
		var err error
		snapshots, err = loadAutoRouteSnapshots(groups)
		return err
	}
	loadRecentSeries := func() error {
		var err error
		recentSeries, err = marketplaceRecentRequestSeries(groups, channels)
		return err
	}
	if platformdb.DB.Dialector.Name() == "sqlite" {
		if err := loadSnapshots(); err != nil {
			return nil, err
		}
		if err := loadRecentSeries(); err != nil {
			return nil, err
		}
	} else {
		var group errgroup.Group
		group.Go(loadSnapshots)
		group.Go(loadRecentSeries)
		if err := group.Wait(); err != nil {
			return nil, err
		}
	}
	selected, err := loadAutoRoutePoolSelection(ownerUserID)
	if err != nil {
		return nil, err
	}
	config := loadAutoRoutePoolConfig(ownerUserID)
	blockedChannels, err := loadBlockedChannelIDs(ownerUserID, groups)
	if err != nil {
		return nil, err
	}

	items := make([]AutoRoutePoolItem, 0, len(groups))
	selectedCount := 0
	for _, group := range groups {
		channel := channels[group.ChannelID]
		if _, blocked := blockedChannels[group.ChannelID]; blocked {
			continue
		}
		priority, isSelected := selected[group.ID]
		if isSelected {
			selectedCount++
		}
		availability, score := autoRouteMetrics(group, snapshots[group.ID], config)
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
			AvgTTFTMs:           round2(snapshot.AvgTTFTMs),
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
		TokenGroup: gatewayroutingapp.AutoGroupName, SelectedCount: selectedCount, Items: items, Config: config,
	}, nil
}

func channelUserBlocked(channelID string, userID int) bool {
	var count int64
	return platformdb.DB.Model(&marketplaceschema.ChannelUserBlock{}).Where("channel_id = ? AND user_id = ?", channelID, userID).Count(&count).Error == nil && count > 0
}

func loadBlockedChannelIDs(userID int, groups []marketplaceschema.Group) (map[string]struct{}, error) {
	channelIDs := make([]string, 0, len(groups))
	seen := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		if group.ChannelID == "" {
			continue
		}
		if _, ok := seen[group.ChannelID]; ok {
			continue
		}
		seen[group.ChannelID] = struct{}{}
		channelIDs = append(channelIDs, group.ChannelID)
	}
	blocked := make(map[string]struct{})
	if len(channelIDs) == 0 || platformdb.DB == nil {
		return blocked, nil
	}
	var rows []marketplaceschema.ChannelUserBlock
	if err := platformdb.DB.Where("user_id = ? AND channel_id IN ?", userID, channelIDs).Find(&rows).Error; err != nil {
		// The block table was added after the initial marketplace schema. Treat
		// an older database as having no blocks until migration catches up.
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "no such table") || strings.Contains(message, "does not exist") {
			return blocked, nil
		}
		return nil, err
	}
	for _, row := range rows {
		blocked[row.ChannelID] = struct{}{}
	}
	return blocked, nil
}

// ReplaceAutoRoutePool atomically replaces the current user's selected groups.
func ReplaceAutoRoutePool(ownerUserID int, req AutoRoutePoolUpdateRequest) (*AutoRoutePoolView, error) {
	groupIDs := normalizeAutoRouteGroupIDs(req.GroupIDs)
	if len(groupIDs) > maxAutoRoutePoolMembers {
		return nil, errors.New("全局 Auto 路由池最多可添加 10 个分组")
	}
	groups, _, err := loadAutoRouteGroupsForIDs(ownerUserID, groupIDs)
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

	config := normalizeAutoRoutePoolConfig(req.Config)
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
		if tx.Migrator().HasTable(&marketplaceschema.AutoRoutePoolConfig{}) {
			if err := tx.Save(&marketplaceschema.AutoRoutePoolConfig{OwnerUserID: ownerUserID, Strategy: config.Strategy, MaxAttempts: config.MaxAttempts, FailureCooldownSeconds: config.FailureCooldownSeconds, MaxMultiplier: config.MaxMultiplier, MultiplierWeight: config.MultiplierWeight, SuccessWeight: config.SuccessWeight, CacheWeight: config.CacheWeight, TTFTWeight: config.TTFTWeight}).Error; err != nil {
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

func loadAutoRoutePoolConfig(ownerUserID int) AutoRoutePoolConfig {
	if !platformdb.DB.Migrator().HasTable(&marketplaceschema.AutoRoutePoolConfig{}) {
		return normalizeAutoRoutePoolConfig(nil)
	}
	var config marketplaceschema.AutoRoutePoolConfig
	if platformdb.DB.First(&config, "owner_user_id = ?", ownerUserID).Error != nil {
		return normalizeAutoRoutePoolConfig(nil)
	}
	return normalizeAutoRoutePoolConfig(&AutoRoutePoolConfig{Strategy: config.Strategy, MaxAttempts: config.MaxAttempts, FailureCooldownSeconds: config.FailureCooldownSeconds, MaxMultiplier: config.MaxMultiplier, MultiplierWeight: config.MultiplierWeight, SuccessWeight: config.SuccessWeight, CacheWeight: config.CacheWeight, TTFTWeight: config.TTFTWeight})
}

func normalizeAutoRoutePoolConfig(config *AutoRoutePoolConfig) AutoRoutePoolConfig {
	result := AutoRoutePoolConfig{Strategy: "priority", MaxAttempts: 3, FailureCooldownSeconds: 30, MultiplierWeight: 35, SuccessWeight: 25, CacheWeight: 15, TTFTWeight: 25}
	if config != nil {
		result = *config
	}
	if result.Strategy != "priority" && result.Strategy != "score" && result.Strategy != "cost" {
		result.Strategy = "priority"
	}
	if result.MaxAttempts < 1 || result.MaxAttempts > 5 {
		result.MaxAttempts = 3
	}
	if result.FailureCooldownSeconds < 5 || result.FailureCooldownSeconds > 3600 {
		result.FailureCooldownSeconds = 30
	}
	if result.MaxMultiplier > 0 && result.MaxMultiplier < 0.001 {
		result.MaxMultiplier = 0.001
	}
	if result.MaxMultiplier < 0 {
		result.MaxMultiplier = 0
	}
	if result.MultiplierWeight < 0 {
		result.MultiplierWeight = 0
	}
	if result.SuccessWeight < 0 {
		result.SuccessWeight = 0
	}
	if result.CacheWeight < 0 {
		result.CacheWeight = 0
	}
	if result.TTFTWeight < 0 {
		result.TTFTWeight = 0
	}
	if result.MultiplierWeight+result.SuccessWeight+result.CacheWeight+result.TTFTWeight == 0 {
		result.MultiplierWeight, result.SuccessWeight, result.CacheWeight, result.TTFTWeight = 35, 25, 15, 25
	}
	return result
}

// ResolveAutoRouteBindings returns model-compatible pool members in routing
// order. The distributor tries them in order and falls through when a group
// has no currently healthy channel.
func ResolveAutoRouteBindings(ownerUserID int, modelName string, multiplierLimit float64) ([]RoutingBinding, error) {
	selected, err := loadAutoRoutePoolSelection(ownerUserID)
	if err != nil {
		return nil, err
	}
	return resolveRoutePoolBindings(ownerUserID, selected, loadAutoRoutePoolConfig(ownerUserID), modelName, multiplierLimit)
}

func resolveRoutePoolBindings(ownerUserID int, selected map[string]int, config AutoRoutePoolConfig, modelName string, multiplierLimit float64) ([]RoutingBinding, error) {
	if err := ValidateMultiplierLimitValue(multiplierLimit); err != nil {
		return nil, err
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil, errors.New("路由池需要模型名称")
	}
	if len(selected) == 0 {
		return nil, errors.New("Auto 路由池为空，请先添加官方或第三方分组")
	}
	if multiplierLimit <= 0 && config.MaxMultiplier > 0 {
		multiplierLimit = config.MaxMultiplier
	}
	marketplaceGroupIDs := make([]string, 0, len(selected))
	for groupID := range selected {
		if !strings.HasPrefix(groupID, officialAutoRoutePrefix) {
			marketplaceGroupIDs = append(marketplaceGroupIDs, groupID)
		}
	}
	groups, channels, err := loadAutoRouteGroupsForIDs(ownerUserID, marketplaceGroupIDs)
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
		effectiveMultiplier := group.Multiplier
		var userOverride marketplaceschema.UserMultiplier
		if err := platformdb.DB.Where("channel_id = ? AND user_id = ?", channel.ID, ownerUserID).First(&userOverride).Error; err == nil && userOverride.Multiplier > 0 {
			effectiveMultiplier = userOverride.Multiplier
		}
		if !MultiplierWithinLimit(effectiveMultiplier, multiplierLimit) {
			overLimitCount++
			continue
		}
		_, score := autoRouteMetrics(group, snapshots[group.ID], config)
		candidates = append(candidates, scoredBinding{
			binding: RoutingBinding{
				RouteKey: group.ID,
				GroupID:  group.ID, InternalGroup: group.InternalGroupName,
				OwnerUserID: group.OwnerUserID, SourceType: group.SourceType,
				CreditPoolPolicy: group.CreditPoolPolicy, Multiplier: effectiveMultiplier,
				ModelPrices: decodeChannelModelPrices(channel.ModelPrices),
				Models:      decodeModels(channel.DeclaredModels),
			},
			score: score, priority: priority,
		})
	}
	for _, item := range loadOfficialAutoRouteItemsForSelection(ownerUserID, selected) {
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
		if config.Strategy == "priority" && candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		if config.Strategy == "cost" && candidates[i].binding.Multiplier != candidates[j].binding.Multiplier {
			return candidates[i].binding.Multiplier < candidates[j].binding.Multiplier
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
		return nil, ErrAutoRouteModelUnavailable
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
	marketplaceGroupIDs := make([]string, 0, len(selected))
	for groupID := range selected {
		if !strings.HasPrefix(groupID, officialAutoRoutePrefix) {
			marketplaceGroupIDs = append(marketplaceGroupIDs, groupID)
		}
	}
	groups, channels, err := loadAutoRouteGroupsForIDs(ownerUserID, marketplaceGroupIDs)
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
	for _, item := range loadOfficialAutoRouteItemsForSelection(ownerUserID, selected) {
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
	return loadAutoRouteGroupsForIDs(ownerUserID, nil)
}

func loadAutoRouteGroupsForIDs(ownerUserID int, groupIDs []string) ([]marketplaceschema.Group, map[string]marketplaceschema.Channel, error) {
	// A non-nil empty slice means the pool contains only official groups. Keep
	// that distinct from nil, which is the caller's request for the full list.
	if groupIDs != nil && len(groupIDs) == 0 {
		return []marketplaceschema.Group{}, map[string]marketplaceschema.Channel{}, nil
	}
	var groups []marketplaceschema.Group
	query := platformdb.DB.Where("source_type = ? AND verification_status = ? AND lifecycle_status IN ?", marketplacedomain.SourceTypeMarketplaceUser, marketplacedomain.VerificationPassed, []string{marketplacedomain.LifecycleActive, marketplacedomain.LifecycleDegraded})
	if groupIDs != nil {
		query = query.Where("id IN ?", groupIDs)
	}
	if platformdb.DB.Migrator().HasTable(&marketplaceschema.GroupAccess{}) {
		query = query.Where(fmt.Sprintf("visibility = ? OR owner_user_id = ? OR EXISTS (SELECT 1 FROM %s ga WHERE ga.group_id = %s.id AND ga.user_id = ?)", marketplaceschema.GroupAccess{}.TableName(), marketplaceschema.Group{}.TableName()), marketplacedomain.VisibilityPublic, ownerUserID, ownerUserID)
	} else {
		query = query.Where("visibility = ? OR owner_user_id = ?", marketplacedomain.VisibilityPublic, ownerUserID)
	}
	err := query.Limit(1000).Find(&groups).Error
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

func autoRouteMetrics(group marketplaceschema.Group, snapshot marketplaceschema.RankingSnapshot, config AutoRoutePoolConfig) (float64, float64) {
	availability := snapshot.WilsonSuccessRate / 100
	if snapshot.RequestCount == 0 {
		availability = 0.85
	}
	if group.LifecycleStatus == marketplacedomain.LifecycleDegraded {
		availability *= 0.8
	}
	availability = math.Max(0.2, math.Min(availability, 1))
	multiplier := math.Max(group.Multiplier, 0.000001)
	weights := float64(config.MultiplierWeight + config.SuccessWeight + config.CacheWeight + config.TTFTWeight)
	if weights <= 0 {
		weights = 100
	}
	multiplierScore := 1 / math.Max(multiplier, 0.1)
	successScore := math.Max(0, math.Min(snapshot.WilsonSuccessRate/100, 1))
	cacheScore := math.Max(0, math.Min(snapshot.CacheHitRate/100, 1))
	latencyScore := 1 / (1 + math.Max(snapshot.AvgTTFTMs, 0)/1000)
	quality := (multiplierScore*float64(config.MultiplierWeight) + successScore*float64(config.SuccessWeight) + cacheScore*float64(config.CacheWeight) + latencyScore*float64(config.TTFTWeight)) / weights
	return availability, 1 / math.Max(quality, 0.001)
}
