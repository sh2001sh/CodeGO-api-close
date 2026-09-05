package app

import (
	"errors"
	"sort"
	"strings"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

const maxNamedRoutePools = 20

func ListRoutePools(ownerUserID int) ([]RoutePoolSummary, error) {
	var pools []marketplaceschema.RoutePool
	if err := platformdb.DB.Where("owner_user_id = ?", ownerUserID).Order("created_at asc").Find(&pools).Error; err != nil {
		return nil, err
	}
	if len(pools) == 0 {
		return []RoutePoolSummary{}, nil
	}
	// A summary only needs selected members and declared models. Loading every
	// pool through ListRoutePool repeats snapshots and recent-series queries.
	groups, channels, err := loadAutoRouteGroups(ownerUserID)
	if err != nil {
		return nil, err
	}
	groupsByID := make(map[string]marketplaceschema.Group, len(groups))
	for _, group := range groups {
		groupsByID[group.ID] = group
	}
	poolIDs := make([]string, 0, len(pools))
	for _, pool := range pools {
		poolIDs = append(poolIDs, pool.ID)
	}
	var members []marketplaceschema.RoutePoolMember
	if err := platformdb.DB.Where("pool_id IN ?", poolIDs).Order("priority asc, id asc").Find(&members).Error; err != nil {
		return nil, err
	}
	selectedByPool := make(map[string][]marketplaceschema.RoutePoolMember, len(pools))
	for _, member := range members {
		selectedByPool[member.PoolID] = append(selectedByPool[member.PoolID], member)
	}
	officialItems := loadOfficialAutoRouteItemsSummary(ownerUserID)
	officialByID := make(map[string]AutoRoutePoolItem, len(officialItems))
	for _, item := range officialItems {
		officialByID[item.GroupID] = item
	}
	result := make([]RoutePoolSummary, 0, len(pools))
	for _, pool := range pools {
		models := make(map[string]string)
		membersForPool := selectedByPool[pool.ID]
		for _, member := range membersForPool {
			if item, ok := officialByID[member.GroupID]; ok {
				for _, model := range item.Models {
					models[strings.ToLower(model)] = model
				}
				continue
			}
			group, ok := groupsByID[member.GroupID]
			if !ok {
				continue
			}
			channel, ok := channels[group.ChannelID]
			if !ok {
				continue
			}
			for _, model := range decodeModels(channel.DeclaredModels) {
				models[strings.ToLower(model)] = model
			}
		}
		values := make([]string, 0, len(models))
		for _, model := range models {
			values = append(values, model)
		}
		sort.Strings(values)
		result = append(result, RoutePoolSummary{ID: pool.ID, Name: pool.Name, TokenGroup: RoutePoolTokenGroupValue(pool.ID), MemberCount: len(membersForPool), Models: values})
	}
	return result, nil
}

func CreateRoutePool(ownerUserID int, req RoutePoolCreateRequest) (*RoutePoolView, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 64 {
		return nil, errors.New("路由池名称需为 1-64 个字符")
	}
	var count int64
	if err := platformdb.DB.Model(&marketplaceschema.RoutePool{}).Where("owner_user_id = ?", ownerUserID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count >= maxNamedRoutePools {
		return nil, errors.New("每个用户最多创建 20 个路由池")
	}
	pool := marketplaceschema.RoutePool{ID: platformruntime.GetUUID(), OwnerUserID: ownerUserID, Name: name, Strategy: "priority", MaxAttempts: 3, FailureCooldownSeconds: 30}
	if err := platformdb.DB.Create(&pool).Error; err != nil {
		return nil, err
	}
	return ListRoutePool(ownerUserID, pool.ID)
}

func ListRoutePool(ownerUserID int, poolID string) (*RoutePoolView, error) {
	pool, selected, err := loadRoutePool(ownerUserID, poolID)
	if err != nil {
		return nil, err
	}
	groups, channels, err := loadAutoRouteGroups(ownerUserID)
	if err != nil {
		return nil, err
	}
	var snapshots map[string]marketplaceschema.RankingSnapshot
	var series map[int][]RecentRequestBucket
	loadSnapshots := func() error {
		var err error
		snapshots, err = loadAutoRouteSnapshots(groups)
		return err
	}
	loadSeries := func() error {
		var err error
		series, err = marketplaceRecentRequestSeries(groups, channels)
		return err
	}
	if platformdb.DB.Dialector.Name() == "sqlite" {
		if err := loadSnapshots(); err != nil {
			return nil, err
		}
		if err := loadSeries(); err != nil {
			return nil, err
		}
	} else {
		var group errgroup.Group
		group.Go(loadSnapshots)
		group.Go(loadSeries)
		if err := group.Wait(); err != nil {
			return nil, err
		}
	}
	config := routePoolConfig(pool)
	items, err := buildRoutePoolItems(ownerUserID, groups, channels, snapshots, series, selected, config)
	if err != nil {
		return nil, err
	}
	return &RoutePoolView{ID: pool.ID, Name: pool.Name, TokenGroup: RoutePoolTokenGroupValue(pool.ID), SelectedCount: len(selected), Items: items, Config: config}, nil
}

func UpdateRoutePool(ownerUserID int, poolID string, req RoutePoolUpdateRequest) (*RoutePoolView, error) {
	pool, _, err := loadRoutePool(ownerUserID, poolID)
	if err != nil {
		return nil, err
	}
	groupIDs := normalizeAutoRouteGroupIDs(req.GroupIDs)
	if len(groupIDs) > maxAutoRoutePoolMembers {
		return nil, errors.New("路由池最多可添加 10 个分组")
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
	config := routePoolConfig(pool)
	if req.Config != nil {
		config = normalizeAutoRoutePoolConfig(req.Config)
	}
	name := strings.TrimSpace(req.Name)
	if name != "" {
		if len([]rune(name)) > 64 {
			return nil, errors.New("路由池名称不能超过 64 个字符")
		}
		pool.Name = name
	}
	pool.Strategy, pool.MaxAttempts, pool.FailureCooldownSeconds, pool.MaxMultiplier = config.Strategy, config.MaxAttempts, config.FailureCooldownSeconds, config.MaxMultiplier
	err = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&pool).Error; err != nil {
			return err
		}
		if err := tx.Where("pool_id = ?", pool.ID).Delete(&marketplaceschema.RoutePoolMember{}).Error; err != nil {
			return err
		}
		for index, groupID := range groupIDs {
			if err := tx.Create(&marketplaceschema.RoutePoolMember{PoolID: pool.ID, GroupID: groupID, Priority: index + 1}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ListRoutePool(ownerUserID, pool.ID)
}

func DeleteRoutePool(ownerUserID int, poolID string) error {
	pool, _, err := loadRoutePool(ownerUserID, poolID)
	if err != nil {
		return err
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("pool_id = ?", pool.ID).Delete(&marketplaceschema.RoutePoolMember{}).Error; err != nil {
			return err
		}
		return tx.Delete(&pool).Error
	})
}

func ResolveRoutePoolBindings(ownerUserID int, poolID, modelName string, multiplierLimit float64) ([]RoutingBinding, error) {
	pool, selected, err := loadRoutePool(ownerUserID, poolID)
	if err != nil {
		return nil, err
	}
	return resolveRoutePoolBindings(ownerUserID, selected, routePoolConfig(pool), modelName, multiplierLimit)
}

func HasRoutePool(ownerUserID int, poolID string) bool {
	_, _, err := loadRoutePool(ownerUserID, poolID)
	return err == nil
}

func ListRoutePoolModels(ownerUserID int, poolID string) ([]string, error) {
	view, err := ListRoutePool(ownerUserID, poolID)
	if err != nil {
		return nil, err
	}
	models := map[string]string{}
	for _, item := range view.Items {
		if item.Selected {
			for _, model := range item.Models {
				models[strings.ToLower(model)] = model
			}
		}
	}
	result := make([]string, 0, len(models))
	for _, model := range models {
		result = append(result, model)
	}
	sort.Strings(result)
	return result, nil
}

func loadRoutePool(ownerUserID int, poolID string) (marketplaceschema.RoutePool, map[string]int, error) {
	var pool marketplaceschema.RoutePool
	if err := platformdb.DB.Where("id = ? AND owner_user_id = ?", strings.TrimSpace(poolID), ownerUserID).First(&pool).Error; err != nil {
		return pool, nil, err
	}
	var members []marketplaceschema.RoutePoolMember
	if err := platformdb.DB.Where("pool_id = ?", pool.ID).Order("priority asc, id asc").Find(&members).Error; err != nil {
		return pool, nil, err
	}
	selected := make(map[string]int, len(members))
	for index, member := range members {
		selected[member.GroupID] = member.Priority
		if member.Priority <= 0 {
			selected[member.GroupID] = index + 1
		}
	}
	return pool, selected, nil
}

func routePoolConfig(pool marketplaceschema.RoutePool) AutoRoutePoolConfig {
	return normalizeAutoRoutePoolConfig(&AutoRoutePoolConfig{Strategy: pool.Strategy, MaxAttempts: pool.MaxAttempts, FailureCooldownSeconds: pool.FailureCooldownSeconds, MaxMultiplier: pool.MaxMultiplier})
}

func buildRoutePoolItems(ownerUserID int, groups []marketplaceschema.Group, channels map[string]marketplaceschema.Channel, snapshots map[string]marketplaceschema.RankingSnapshot, series map[int][]RecentRequestBucket, selected map[string]int, config AutoRoutePoolConfig) ([]AutoRoutePoolItem, error) {
	blockedChannels, err := loadBlockedChannelIDs(ownerUserID, groups)
	if err != nil {
		return nil, err
	}
	items := make([]AutoRoutePoolItem, 0, len(groups))
	for _, group := range groups {
		channel := channels[group.ChannelID]
		if _, blocked := blockedChannels[group.ChannelID]; blocked {
			continue
		}
		if config.MaxMultiplier > 0 && !MultiplierWithinLimit(group.Multiplier, config.MaxMultiplier) {
			// Keep the editor consistent with runtime resolution: a group above
			// the configured ceiling must not remain selectable after its public
			// multiplier changes.
			continue
		}
		priority, isSelected := selected[group.ID]
		availability, score := autoRouteMetrics(group, snapshots[group.ID], config)
		channelID := 0
		if channel.InternalChannelID != nil {
			channelID = *channel.InternalChannelID
		}
		snapshot := snapshots[group.ID]
		items = append(items, AutoRoutePoolItem{GroupID: group.ID, SourceType: marketplacedomain.SourceTypeMarketplaceUser, PublicSlug: group.PublicSlug, SystemDisplayName: marketplaceDisplayName(publicSourceLabel(channel), group.Multiplier, channel.ID), SourceLabel: publicSourceLabel(channel), LifecycleStatus: group.LifecycleStatus, Multiplier: group.Multiplier, Availability: round2(availability * 100), SuccessRate: round2(snapshot.RawSuccessRate), CacheHitRate: round2(snapshot.CacheHitRate), AvgTTFTMs: round2(snapshot.AvgTTFTMs), AvgLatencyMS: round2(snapshot.AvgLatencyMs), LatestRequestStatus: latestRequestStatus(series[channelID]), MetricsAvailable: snapshot.RequestCount > 0, RouteScore: round2(score), Observing: snapshot.Observing, RequestCount: snapshot.RequestCount, Models: decodeModels(channel.DeclaredModels), Selected: isSelected, Priority: priority})
	}
	items = append(items, loadOfficialAutoRouteItems(ownerUserID, selected)...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Selected != items[j].Selected {
			return items[i].Selected
		}
		if items[i].Selected && items[i].Priority != items[j].Priority {
			return items[i].Priority < items[j].Priority
		}
		return items[i].GroupID < items[j].GroupID
	})
	return items, nil
}
