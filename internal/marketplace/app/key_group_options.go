package app

import (
	"errors"
	"sort"
	"strings"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

// KeyGroupOption contains selector metadata only; request logs, ranking and
// channel verification evidence are deliberately not loaded by this endpoint.
type KeyGroupOption struct {
	Value                  string   `json:"value"`
	Label                  string   `json:"label"`
	Category               string   `json:"category"`
	Multiplier             *float64 `json:"multiplier,omitempty"`
	SubscriptionEnabled    bool     `json:"subscription_enabled,omitempty"`
	SubscriptionMultiplier float64  `json:"subscription_multiplier,omitempty"`
	MappingStatus          string   `json:"mapping_status,omitempty"`
	Models                 []string `json:"models"`
	MemberCount            int      `json:"member_count,omitempty"`
	SuccessRate            *float64 `json:"success_rate,omitempty"`
	AvgTTFTMs              *float64 `json:"avg_ttft_ms,omitempty"`
	RequestCount           int64    `json:"request_count,omitempty"`
}

func ListKeyGroupOptions(userID int) ([]KeyGroupOption, error) {
	if userID <= 0 {
		return nil, errors.New("请先登录")
	}
	groups, err := loadPublicGroupRows(GroupQuery{ViewerUserID: userID, IncludeAccess: true, Verification: marketplacedomain.VerificationPassed})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(groups))
	for _, group := range groups {
		ids = append(ids, group.ChannelID)
	}
	var channels []marketplaceschema.Channel
	var overrides []marketplaceschema.UserMultiplier
	if len(ids) > 0 {
		if err := platformdb.DB.Select("id, approved_source_label, source_label_status, declared_models, gpt56_mapping_status, internal_channel_id").Where("id IN ?", ids).Find(&channels).Error; err != nil {
			return nil, err
		}
		// One query for all personal prices, rather than one query per option.
		if err := platformdb.DB.Select("channel_id, multiplier").Where("user_id = ? AND channel_id IN ?", userID, ids).Find(&overrides).Error; err != nil {
			return nil, err
		}
	}
	channelsByID := make(map[string]marketplaceschema.Channel, len(channels))
	for _, channel := range channels {
		channelsByID[channel.ID] = channel
	}
	prices := make(map[string]float64, len(overrides))
	for _, override := range overrides {
		prices[override.ChannelID] = override.Multiplier
	}
	blocked, err := loadBlockedChannelIDs(userID, groups)
	if err != nil {
		return nil, err
	}
	options := make([]KeyGroupOption, 0, len(groups))
	// Statistics are optional selector metadata. A cold install may not have
	// the snapshot table yet; group selection must still work without it.
	snapshots, _ := rankingSnapshotsForList(groups, channelsByID, 24)
	modelsByGroup := make(map[string][]string, len(groups))
	for _, group := range groups {
		channel, exists := channelsByID[group.ChannelID]
		_, isBlocked := blocked[group.ChannelID]
		if !exists || isBlocked || channel.GPT56MappingStatus == "mismatch" ||
			(group.LifecycleStatus != marketplacedomain.LifecycleActive && group.LifecycleStatus != marketplacedomain.LifecycleDegraded) {
			continue
		}
		models := decodeModels(channel.DeclaredModels)
		multiplier := marketplacedomain.NormalizeMultiplier(group.Multiplier)
		label := marketplaceDisplayName(publicSourceLabel(channel), multiplier, channel.ID)
		if price := prices[channel.ID]; userID != group.OwnerUserID && price > 0 {
			multiplier = marketplacedomain.NormalizeMultiplier(price)
		}
		option := KeyGroupOption{
			Value: "market:" + group.ID, Label: label, Category: "marketplace",
			Multiplier: &multiplier, Models: models, MappingStatus: channel.GPT56MappingStatus,
			SubscriptionEnabled:    group.CreditPoolPolicy == marketplacedomain.CreditPolicySubscriptionAndUniversal,
			SubscriptionMultiplier: marketplacedomain.SubscriptionMultiplier(multiplier),
		}
		if snapshot, ok := snapshots[group.ID]; ok && snapshot.RequestCount > 0 {
			successRate, ttft := snapshot.RawSuccessRate, snapshot.AvgTTFTMs
			option.SuccessRate = &successRate
			option.AvgTTFTMs = &ttft
			option.RequestCount = snapshot.RequestCount
		}
		options = append(options, option)
		if channel.InternalChannelID != nil && len(models) > 0 && group.SourceType == marketplacedomain.SourceTypeMarketplaceUser {
			modelsByGroup[group.ID] = models
		}
	}

	var pools []marketplaceschema.RoutePool
	if err := platformdb.DB.Select("id, name").Where("owner_user_id = ?", userID).Order("created_at ASC, id ASC").Find(&pools).Error; err != nil {
		return nil, err
	}
	poolIDs := make([]string, 0, len(pools))
	for _, pool := range pools {
		poolIDs = append(poolIDs, pool.ID)
	}
	var members []marketplaceschema.RoutePoolMember
	if len(poolIDs) > 0 {
		if err := platformdb.DB.Select("pool_id, group_id").Where("pool_id IN ?", poolIDs).Find(&members).Error; err != nil {
			return nil, err
		}
	}
	selected, err := loadAutoRoutePoolSelection(userID)
	if err != nil {
		return nil, err
	}
	membersByPool := make(map[string][]string, len(pools))
	needsOfficial := false
	for _, member := range members {
		membersByPool[member.PoolID] = append(membersByPool[member.PoolID], member.GroupID)
		needsOfficial = needsOfficial || strings.HasPrefix(member.GroupID, officialAutoRoutePrefix)
	}
	autoIDs := make([]string, 0, len(selected))
	for id := range selected {
		autoIDs = append(autoIDs, id)
		needsOfficial = needsOfficial || strings.HasPrefix(id, officialAutoRoutePrefix)
	}
	if needsOfficial {
		for _, item := range loadOfficialAutoRouteItemsSummary(userID) {
			modelsByGroup[item.GroupID] = item.Models
		}
	}
	poolOptions := make([]KeyGroupOption, 0, len(pools)+1)
	poolOptions = append(poolOptions, keyPoolOption("market:auto", "AUTO 路由池", "marketplace_auto", autoIDs, modelsByGroup))
	for _, pool := range pools {
		poolOptions = append(poolOptions, keyPoolOption(RoutePoolTokenGroupValue(pool.ID), pool.Name, "marketplace_pool", membersByPool[pool.ID], modelsByGroup))
	}
	return append(poolOptions, options...), nil
}

func keyPoolOption(value, name, category string, members []string, modelsByGroup map[string][]string) KeyGroupOption {
	option := KeyGroupOption{Value: value, Label: name, Category: category, Models: []string{}}
	seenMembers := make(map[string]bool, len(members))
	models := make(map[string]string)
	for _, id := range members {
		values, available := modelsByGroup[id]
		if !available || seenMembers[id] {
			continue
		}
		seenMembers[id] = true
		option.MemberCount++
		for _, model := range values {
			models[strings.ToLower(model)] = model
		}
	}
	for _, model := range models {
		option.Models = append(option.Models, model)
	}
	sort.Strings(option.Models)
	return option
}
