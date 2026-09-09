package app

import (
	"errors"
	"sort"
	"strings"
	"time"

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
	groupIDs := normalizeAutoRouteGroupIDs(req.GroupIDs)
	if len(groupIDs) > maxAutoRoutePoolMembers {
		return nil, errors.New("路由池最多可添加 10 个分组")
	}
	var count int64
	if err := platformdb.DB.Model(&marketplaceschema.RoutePool{}).Where("owner_user_id = ?", ownerUserID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count >= maxNamedRoutePools {
		return nil, errors.New("每个用户最多创建 20 个路由池")
	}
	config := normalizeAutoRoutePoolConfig(req.Config)
	pool := marketplaceschema.RoutePool{ID: platformruntime.GetUUID(), OwnerUserID: ownerUserID, Name: name, Strategy: config.Strategy, MaxAttempts: config.MaxAttempts, FailureCooldownSeconds: config.FailureCooldownSeconds, MaxMultiplier: config.MaxMultiplier}
	if req.AutoBuild != nil {
		applyRoutePoolAutoBuild(&pool, *req.AutoBuild)
	}
	if len(groupIDs) > 0 {
		groups, _, err := loadAutoRouteGroupsForIDs(ownerUserID, groupIDs)
		if err != nil {
			return nil, err
		}
		eligible := make(map[string]struct{}, len(groups))
		for _, group := range groups {
			eligible[group.ID] = struct{}{}
		}
		for _, groupID := range groupIDs {
			if strings.HasPrefix(groupID, officialAutoRoutePrefix) {
				for _, item := range loadOfficialAutoRouteItemsSummary(ownerUserID) {
					eligible[item.GroupID] = struct{}{}
				}
				break
			}
		}
		validGroupIDs := make([]string, 0, len(groupIDs))
		for _, groupID := range groupIDs {
			if _, ok := eligible[groupID]; ok {
				validGroupIDs = append(validGroupIDs, groupID)
			}
		}
		groupIDs = validGroupIDs
		if len(groupIDs) == 0 {
			return nil, errors.New("没有可用的分组可加入路由池")
		}
	}
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&pool).Error; err != nil {
			message := strings.ToLower(err.Error())
			if strings.Contains(message, "uq_marketplace_route_pool_name") || strings.Contains(message, "duplicate key") || strings.Contains(message, "unique constraint") {
				return errors.New("路由池名称已存在，请使用其他名称")
			}
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
	// Metrics are loaded by the detail query after the pool is selected. Avoid
	// repeating the expensive snapshot and request-series queries during create.
	return &RoutePoolView{ID: pool.ID, Name: pool.Name, TokenGroup: RoutePoolTokenGroupValue(pool.ID), SelectedCount: len(groupIDs), Config: config, AutoBuild: routePoolAutoBuildConfig(pool)}, nil
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
	return &RoutePoolView{ID: pool.ID, Name: pool.Name, TokenGroup: RoutePoolTokenGroupValue(pool.ID), SelectedCount: len(selected), Items: items, Config: config, AutoBuild: routePoolAutoBuildConfig(pool)}, nil
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
	if req.AutoBuild != nil {
		applyRoutePoolAutoBuild(&pool, *req.AutoBuild)
	}
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
		// DELETE is intentionally idempotent so a stale detail request or a
		// repeated click cannot surface a misleading "record not found" error.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("pool_id = ?", pool.ID).Delete(&marketplaceschema.RoutePoolMember{}).Error; err != nil {
			return err
		}
		return tx.Delete(&pool).Error
	})
}

func ResolveRoutePoolBindings(ownerUserID int, poolID, modelName string, multiplierLimit float64) ([]RoutingBinding, string, error) {
	pool, selected, err := loadRoutePool(ownerUserID, poolID)
	if err != nil {
		return nil, "", err
	}
	bindings, err := resolveRoutePoolBindings(ownerUserID, selected, routePoolConfig(pool), modelName, multiplierLimit)
	return bindings, pool.Name, err
}

func HasRoutePool(ownerUserID int, poolID string) bool {
	_, _, err := loadRoutePool(ownerUserID, poolID)
	return err == nil
}

func ListRoutePoolModels(ownerUserID int, poolID string) ([]string, error) {
	_, selected, err := loadRoutePool(ownerUserID, poolID)
	if err != nil {
		return nil, err
	}
	models := map[string]string{}
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
	for _, group := range groups {
		if _, ok := selected[group.ID]; !ok {
			continue
		}
		for _, model := range decodeModels(channels[group.ChannelID].DeclaredModels) {
			model = strings.TrimSpace(model)
			if model != "" {
				models[strings.ToLower(model)] = model
			}
		}
	}
	for _, model := range loadOfficialAutoRouteModels(ownerUserID, selected) {
		model = strings.TrimSpace(model)
		if model != "" {
			models[strings.ToLower(model)] = model
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

func routePoolAutoBuildConfig(pool marketplaceschema.RoutePool) RoutePoolAutoBuildConfig {
	return RoutePoolAutoBuildConfig{Enabled: pool.AutoBuildEnabled, Schedule: pool.AutoBuildSchedule, IntervalMinutes: pool.AutoBuildInterval, DailyTime: pool.AutoBuildDailyTime, Model: pool.AutoBuildModel, Size: pool.AutoBuildSize, Explore: pool.AutoBuildExplore, LastBuiltAt: pool.AutoBuildLastAt, NextBuildAt: pool.AutoBuildNextAt, LastError: pool.AutoBuildLastError}
}

func applyRoutePoolAutoBuild(pool *marketplaceschema.RoutePool, cfg RoutePoolAutoBuildConfig) {
	pool.AutoBuildEnabled = cfg.Enabled
	pool.AutoBuildSchedule = cfg.Schedule
	if pool.AutoBuildSchedule != "daily" {
		pool.AutoBuildSchedule = "interval"
	}
	pool.AutoBuildInterval = cfg.IntervalMinutes
	if pool.AutoBuildInterval < 15 {
		pool.AutoBuildInterval = 15
	}
	if pool.AutoBuildInterval > 10080 {
		pool.AutoBuildInterval = 10080
	}
	pool.AutoBuildDailyTime = strings.TrimSpace(cfg.DailyTime)
	if len(pool.AutoBuildDailyTime) != 5 {
		pool.AutoBuildDailyTime = "03:00"
	}
	pool.AutoBuildModel = strings.TrimSpace(cfg.Model)
	pool.AutoBuildSize = cfg.Size
	if pool.AutoBuildSize < 1 {
		pool.AutoBuildSize = 1
	}
	if pool.AutoBuildSize > maxAutoRoutePoolMembers {
		pool.AutoBuildSize = maxAutoRoutePoolMembers
	}
	pool.AutoBuildExplore = cfg.Explore
	if pool.AutoBuildExplore < 0 {
		pool.AutoBuildExplore = 0
	}
	if pool.AutoBuildExplore > maxAutoRoutePoolMembers {
		pool.AutoBuildExplore = maxAutoRoutePoolMembers
	}
	if pool.AutoBuildEnabled {
		next := nextRoutePoolAutoBuild(*pool, time.Now().UTC())
		pool.AutoBuildNextAt = &next
	}
	if !pool.AutoBuildEnabled {
		pool.AutoBuildNextAt = nil
	}
}

func RunRoutePoolAutoBuild(ownerUserID int, poolID string) (*RoutePoolView, error) {
	pool, _, err := loadRoutePool(ownerUserID, poolID)
	if err != nil {
		return nil, err
	}
	all, err := ListAutoRoutePool(ownerUserID)
	if err != nil {
		return nil, err
	}
	model := strings.ToLower(strings.TrimSpace(pool.AutoBuildModel))
	candidates := make([]AutoRoutePoolItem, 0, len(all.Items))
	for _, item := range all.Items {
		if model == "" || containsModel(item.Models, model) {
			candidates = append(candidates, item)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].RouteScore < candidates[j].RouteScore })
	size := pool.AutoBuildSize
	if size < 1 {
		size = 3
	}
	if size > len(candidates) {
		size = len(candidates)
	}
	explore := pool.AutoBuildExplore
	if explore < 0 {
		explore = 0
	}
	selected := make([]string, 0, size+explore)
	for i := 0; i < size; i++ {
		selected = append(selected, candidates[i].GroupID)
	}
	for i := size; i < len(candidates) && len(selected) < size+explore; i++ {
		if candidates[i].Observing || candidates[i].RequestCount == 0 {
			selected = append(selected, candidates[i].GroupID)
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("没有符合条件的可用分组")
	}
	if err := replaceRoutePoolMembers(pool.ID, selected); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	next := nextRoutePoolAutoBuild(pool, now)
	if err := platformdb.DB.Model(&pool).Updates(map[string]interface{}{"auto_build_last_at": now, "auto_build_next_at": next, "auto_build_last_error": ""}).Error; err != nil {
		return nil, err
	}
	return ListRoutePool(ownerUserID, pool.ID)
}

func containsModel(models []string, target string) bool {
	for _, model := range models {
		if strings.ToLower(strings.TrimSpace(model)) == target {
			return true
		}
	}
	return false
}
func replaceRoutePoolMembers(poolID string, ids []string) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("pool_id = ?", poolID).Delete(&marketplaceschema.RoutePoolMember{}).Error; err != nil {
			return err
		}
		for i, id := range ids {
			if err := tx.Create(&marketplaceschema.RoutePoolMember{PoolID: poolID, GroupID: id, Priority: i + 1}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
func nextRoutePoolAutoBuild(pool marketplaceschema.RoutePool, now time.Time) time.Time {
	if pool.AutoBuildSchedule != "daily" {
		return now.Add(time.Duration(pool.AutoBuildInterval) * time.Minute)
	}
	parsed, err := time.ParseInLocation("15:04", pool.AutoBuildDailyTime, time.Local)
	if err != nil {
		return now.Add(24 * time.Hour)
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, time.Local)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next.UTC()
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
