package store

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sh2001sh/new-api/constant"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

// RoutePoolDetail is a root-only configuration view. It never includes channel keys.
type RoutePoolDetail struct {
	Pool    gatewayschema.RoutePool         `json:"pool"`
	Members []gatewayschema.RoutePoolMember `json:"members"`
}

const routePoolCacheTTL = 15 * time.Second

type routePoolCacheEntry struct {
	detail    *RoutePoolDetail
	expiresAt time.Time
}

var routePoolCache struct {
	sync.RWMutex
	byGroup map[string]routePoolCacheEntry
}

func ListRoutePools() ([]RoutePoolDetail, error) {
	var pools []gatewayschema.RoutePool
	if err := platformdb.DB.Order("id asc").Find(&pools).Error; err != nil {
		return nil, err
	}
	details := make([]RoutePoolDetail, 0, len(pools))
	for _, pool := range pools {
		members, err := listRoutePoolMembers(pool.ID)
		if err != nil {
			return nil, err
		}
		details = append(details, RoutePoolDetail{Pool: pool, Members: members})
	}
	return details, nil
}

func LoadEnabledRoutePool(group string) (*RoutePoolDetail, error) {
	group = strings.TrimSpace(group)
	if group == "" || platformdb.DB == nil {
		return nil, nil
	}
	now := time.Now()
	routePoolCache.RLock()
	cached, found := routePoolCache.byGroup[group]
	routePoolCache.RUnlock()
	if found && cached.detail != nil && now.Before(cached.expiresAt) {
		return cloneRoutePoolDetail(cached.detail), nil
	}

	var pool gatewayschema.RoutePool
	err := platformdb.DB.Where(routePoolGroupColumn()+" = ? AND enabled = ?", group, true).First(&pool).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	members, err := listRoutePoolMembers(pool.ID)
	if err != nil {
		return nil, err
	}
	detail := &RoutePoolDetail{Pool: pool, Members: members}
	routePoolCache.Lock()
	if routePoolCache.byGroup == nil {
		routePoolCache.byGroup = make(map[string]routePoolCacheEntry)
	}
	routePoolCache.byGroup[group] = routePoolCacheEntry{
		detail:    detail,
		expiresAt: now.Add(routePoolCacheTTL),
	}
	routePoolCache.Unlock()
	return cloneRoutePoolDetail(detail), nil
}

func SaveRoutePool(pool *gatewayschema.RoutePool, members []gatewayschema.RoutePoolMember) (*RoutePoolDetail, error) {
	if pool == nil {
		return nil, errors.New("route pool is required")
	}
	pool.Name = strings.TrimSpace(pool.Name)
	pool.Group = strings.TrimSpace(pool.Group)
	if pool.Name == "" || pool.Group == "" {
		return nil, errors.New("route pool name and group are required")
	}
	if err := validateRoutePoolMembers(members); err != nil {
		return nil, err
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var existing int64
		query := tx.Model(&gatewayschema.RoutePool{}).Where(routePoolGroupColumn()+" = ?", pool.Group)
		if pool.ID > 0 {
			query = query.Where("id <> ?", pool.ID)
		}
		if err := query.Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return errors.New("only one route pool may be configured for a group")
		}
		if pool.ID == 0 {
			if err := tx.Select("Name", "Group", "Enabled", "AutoDiscover").Create(pool).Error; err != nil {
				return err
			}
		} else if err := tx.Model(&gatewayschema.RoutePool{}).Where("id = ?", pool.ID).
			Updates(map[string]any{"name": pool.Name, "group": pool.Group, "enabled": pool.Enabled, "auto_discover": pool.AutoDiscover}).Error; err != nil {
			return err
		}
		// Members are replaced as a complete set. The database-level uniqueness
		// constraint intentionally remains in force for soft-deleted rows, so a
		// normal GORM delete would make reinserting an unchanged channel fail.
		if err := tx.Unscoped().Where("route_pool_id = ?", pool.ID).Delete(&gatewayschema.RoutePoolMember{}).Error; err != nil {
			return err
		}
		for index := range members {
			members[index].ID = 0
			members[index].RoutePoolID = pool.ID
			members[index].FaultDomain = strings.TrimSpace(members[index].FaultDomain)
			member := members[index]
			if err := tx.Model(&gatewayschema.RoutePoolMember{}).Create(map[string]any{
				"route_pool_id":        member.RoutePoolID,
				"channel_id":           member.ChannelID,
				"cost_multiplier":      member.CostMultiplier,
				"model_cost_overrides": member.ModelCostOverrides,
				"fault_domain":         member.FaultDomain,
				"enabled":              member.Enabled,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	InvalidateRoutePoolCache()
	return &RoutePoolDetail{Pool: *pool, Members: members}, nil
}

func DeleteRoutePool(id int64) error {
	if id <= 0 {
		return errors.New("invalid route pool id")
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("route_pool_id = ?", id).Delete(&gatewayschema.RoutePoolMember{}).Error; err != nil {
			return err
		}
		return tx.Delete(&gatewayschema.RoutePool{}, id).Error
	}); err != nil {
		return err
	}
	InvalidateRoutePoolCache()
	return nil
}

func InvalidateRoutePoolCache() {
	routePoolCache.Lock()
	routePoolCache.byGroup = nil
	routePoolCache.Unlock()
}

// UpdateRoutePoolMemberCostMultipliers updates explicitly configured member costs
// without changing their enabled state or model-specific manual overrides.
// Missing channels are returned so callers cannot silently report a successful
// update for an unconfigured route-pool member.
func UpdateRoutePoolMemberCostMultipliers(updates map[int]float64, epsilon float64) (int, []int, error) {
	if len(updates) == 0 {
		return 0, nil, nil
	}
	if epsilon < 0 {
		epsilon = 0
	}

	channelIDs := make([]int, 0, len(updates))
	for channelID, multiplier := range updates {
		if channelID <= 0 || multiplier <= 0 {
			return 0, nil, errors.New("route pool cost updates require positive channel ids and multipliers")
		}
		channelIDs = append(channelIDs, channelID)
	}
	sort.Ints(channelIDs)

	changed := 0
	missing := make([]int, 0)
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		for _, channelID := range channelIDs {
			var members []gatewayschema.RoutePoolMember
			if err := tx.Where("channel_id = ?", channelID).Find(&members).Error; err != nil {
				return err
			}
			if len(members) == 0 {
				missing = append(missing, channelID)
				continue
			}
			for _, member := range members {
				if math.Abs(member.CostMultiplier-updates[channelID]) <= epsilon {
					continue
				}
				if err := tx.Model(&gatewayschema.RoutePoolMember{}).
					Where("id = ?", member.ID).
					Update("cost_multiplier", updates[channelID]).Error; err != nil {
					return err
				}
				changed++
			}
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	if changed > 0 {
		InvalidateRoutePoolCache()
	}
	return changed, missing, nil
}

// UpdateRoutePoolMemberFaultDomains keeps a managed upstream's current shared
// fault boundary aligned across every pool membership. It leaves user-managed
// channels untouched because callers must provide explicit channel IDs.
func UpdateRoutePoolMemberFaultDomains(updates map[int]string) (int, []int, error) {
	if len(updates) == 0 {
		return 0, nil, nil
	}

	channelIDs := make([]int, 0, len(updates))
	for channelID, domain := range updates {
		if channelID <= 0 || strings.TrimSpace(domain) == "" {
			return 0, nil, errors.New("route pool fault domain updates require positive channel ids and domains")
		}
		channelIDs = append(channelIDs, channelID)
	}
	sort.Ints(channelIDs)

	changed := 0
	missing := make([]int, 0)
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		for _, channelID := range channelIDs {
			var members []gatewayschema.RoutePoolMember
			if err := tx.Where("channel_id = ?", channelID).Find(&members).Error; err != nil {
				return err
			}
			if len(members) == 0 {
				missing = append(missing, channelID)
				continue
			}
			for _, member := range members {
				if strings.EqualFold(strings.TrimSpace(member.FaultDomain), strings.TrimSpace(updates[channelID])) {
					continue
				}
				if err := tx.Model(&gatewayschema.RoutePoolMember{}).
					Where("id = ?", member.ID).
					Update("fault_domain", strings.TrimSpace(updates[channelID])).Error; err != nil {
					return err
				}
				changed++
			}
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	if changed > 0 {
		InvalidateRoutePoolCache()
	}
	return changed, missing, nil
}

func listRoutePoolMembers(poolID int64) ([]gatewayschema.RoutePoolMember, error) {
	var members []gatewayschema.RoutePoolMember
	err := platformdb.DB.Where("route_pool_id = ?", poolID).Order("id asc").Find(&members).Error
	return members, err
}

func validateRoutePoolMembers(members []gatewayschema.RoutePoolMember) error {
	seen := make(map[int]struct{}, len(members))
	for index := range members {
		member := &members[index]
		if member.ChannelID <= 0 {
			return errors.New("route pool member channel id must be positive")
		}
		if member.CostMultiplier <= 0 {
			return errors.New("route pool member cost multiplier must be positive")
		}
		if _, exists := seen[member.ChannelID]; exists {
			return errors.New("route pool cannot include the same channel twice")
		}
		seen[member.ChannelID] = struct{}{}
		if strings.TrimSpace(member.ModelCostOverrides) == "" {
			member.ModelCostOverrides = "{}"
		}
		if len(strings.TrimSpace(member.FaultDomain)) > 128 {
			return errors.New("route pool member fault domain exceeds 128 characters")
		}
		var overrides map[string]float64
		if err := json.Unmarshal([]byte(member.ModelCostOverrides), &overrides); err != nil {
			return errors.New("model cost overrides must be a JSON object")
		}
		for model, multiplier := range overrides {
			if strings.TrimSpace(model) == "" || multiplier <= 0 {
				return errors.New("model cost override keys and values must be positive")
			}
		}
	}
	return nil
}

// LoadRoutePoolCandidates applies channel and ability eligibility without exposing
// any sensitive channel data to a caller outside the gateway runtime.
func LoadRoutePoolCandidates(group, modelName string, detail *RoutePoolDetail) ([]RoutePoolCandidate, error) {
	if detail == nil || !detail.Pool.Enabled {
		return nil, nil
	}
	members := detail.Members
	if detail.Pool.AutoDiscover {
		var err error
		members, err = ExpandRoutePoolMembers(group, members)
		if err != nil {
			return nil, err
		}
	}
	candidates := make([]RoutePoolCandidate, 0, len(members))
	for _, member := range members {
		if !member.Enabled || !IsChannelEnabledForGroupModel(group, modelName, member.ChannelID) {
			continue
		}
		channel, err := GetCachedChannel(member.ChannelID)
		if err != nil || channel == nil || channel.Status != constant.ChannelStatusEnabled {
			continue
		}
		candidates = append(candidates, RoutePoolCandidate{Channel: channel, Member: member})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Channel.Id < candidates[j].Channel.Id })
	return candidates, nil
}

// ExpandRoutePoolMembers adds group-assigned channels that have no explicit
// member row. Explicit rows remain authoritative for disable and model-cost
// overrides, while newly added group channels use the neutral cost multiplier.
func ExpandRoutePoolMembers(group string, configured []gatewayschema.RoutePoolMember) ([]gatewayschema.RoutePoolMember, error) {
	byChannelID := make(map[int]gatewayschema.RoutePoolMember, len(configured))
	for _, member := range configured {
		byChannelID[member.ChannelID] = member
	}
	channels, err := listChannelsAssignedToGroup(group)
	if err != nil {
		return nil, err
	}
	members := make([]gatewayschema.RoutePoolMember, 0, len(channels))
	for _, channel := range channels {
		member, found := byChannelID[channel.Id]
		if !found {
			member = gatewayschema.RoutePoolMember{
				ChannelID:          channel.Id,
				CostMultiplier:     1,
				ModelCostOverrides: "{}",
				Enabled:            true,
			}
		}
		members = append(members, member)
	}
	return members, nil
}

func listChannelsAssignedToGroup(group string) ([]*gatewayschema.Channel, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return nil, nil
	}
	if platformconfig.MemoryCacheEnabled {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		channels := make([]*gatewayschema.Channel, 0)
		for _, channel := range channelsIDM {
			for _, channelGroup := range channel.GetGroups() {
				if channelGroup == group {
					channels = append(channels, channel)
					break
				}
			}
		}
		sort.Slice(channels, func(i, j int) bool { return channels[i].Id < channels[j].Id })
		return channels, nil
	}

	var allChannels []*gatewayschema.Channel
	if err := platformdb.DB.Omit("key").Find(&allChannels).Error; err != nil {
		return nil, err
	}
	channels := make([]*gatewayschema.Channel, 0)
	for _, channel := range allChannels {
		for _, channelGroup := range channel.GetGroups() {
			if channelGroup == group {
				channels = append(channels, channel)
				break
			}
		}
	}
	sort.Slice(channels, func(i, j int) bool { return channels[i].Id < channels[j].Id })
	return channels, nil
}

type RoutePoolCandidate struct {
	Channel *gatewayschema.Channel
	Member  gatewayschema.RoutePoolMember
}

func cloneRoutePoolDetail(detail *RoutePoolDetail) *RoutePoolDetail {
	if detail == nil {
		return nil
	}
	clone := *detail
	clone.Members = append([]gatewayschema.RoutePoolMember(nil), detail.Members...)
	return &clone
}

func routePoolGroupColumn() string {
	if platformdb.UsingPostgreSQL {
		return `"group"`
	}
	return "`group`"
}
