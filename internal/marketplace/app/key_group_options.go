package app

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"golang.org/x/sync/singleflight"
)

// KeyGroupOption contains selector metadata and existing ranking snapshots only;
// this endpoint never rebuilds rankings or loads logs or verification evidence.
type KeyGroupOption struct {
	Value                  string   `json:"value"`
	Label                  string   `json:"label"`
	Category               string   `json:"category"`
	Multiplier             *float64 `json:"multiplier,omitempty"`
	SubscriptionEnabled    bool     `json:"subscription_enabled,omitempty"`
	SubscriptionMultiplier float64  `json:"subscription_multiplier,omitempty"`
	Models                 []string `json:"models"`
	MemberCount            int      `json:"member_count,omitempty"`
	SuccessRate            *float64 `json:"success_rate,omitempty"`
	AvgTTFTMs              *float64 `json:"avg_ttft_ms,omitempty"`
	RequestCount           int64    `json:"request_count,omitempty"`
}

var keyGroupOptionLoads singleflight.Group
var keyGroupRankingLoads singleflight.Group

func ListKeyGroupOptions(userID int) ([]KeyGroupOption, error) {
	if userID <= 0 {
		return nil, errors.New("请先登录")
	}
	value, err, _ := keyGroupOptionLoads.Do(fmt.Sprintf("%p:%d", platformdb.DB, userID), func() (any, error) {
		return listKeyGroupOptions(userID)
	})
	if err != nil {
		return nil, err
	}
	return value.([]KeyGroupOption), nil
}

func listKeyGroupOptions(userID int) ([]KeyGroupOption, error) {
	groups, err := loadKeyGroupOptionRows(userID)
	if err != nil {
		return nil, err
	}
	options := make([]KeyGroupOption, 0, len(groups))
	// Statistics are optional selector metadata. A cold install may not have
	// the snapshot table yet; group selection must still work without it.
	snapshots, snapshotErr := loadKeyGroupRankingSnapshots(groups)
	if snapshotErr != nil && !strings.Contains(strings.ToLower(snapshotErr.Error()), "no such table") && !strings.Contains(strings.ToLower(snapshotErr.Error()), "does not exist") {
		return nil, snapshotErr
	}
	modelsByGroup := make(map[string][]string, len(groups))
	for _, group := range groups {
		channel := marketplaceschema.Channel{ID: group.ChannelID, ApprovedSourceLabel: group.ApprovedSourceLabel, SourceLabelStatus: group.SourceLabelStatus, DeclaredModels: group.DeclaredModels, InternalChannelID: group.InternalChannelID}
		models := decodeModels(channel.DeclaredModels)
		multiplier := marketplacedomain.NormalizeMultiplier(group.Multiplier)
		label := marketplaceDisplayName(publicSourceLabel(channel), multiplier, channel.ID)
		if price := group.PersonalMultiplier; userID != group.OwnerUserID && price > 0 {
			multiplier = marketplacedomain.NormalizeMultiplier(discountedMarketplaceMultiplier(group.Multiplier, price))
		}
		option := KeyGroupOption{
			Value: "market:" + group.ID, Label: label, Category: "marketplace",
			Multiplier: &multiplier, Models: models,
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
	officialIDs := make(map[string]int)
	for _, member := range members {
		membersByPool[member.PoolID] = append(membersByPool[member.PoolID], member.GroupID)
		if strings.HasPrefix(member.GroupID, officialAutoRoutePrefix) {
			officialIDs[member.GroupID] = 0
		}
	}
	autoIDs := make([]string, 0, len(selected))
	for id := range selected {
		autoIDs = append(autoIDs, id)
		if strings.HasPrefix(id, officialAutoRoutePrefix) {
			officialIDs[id] = 0
		}
	}
	if len(officialIDs) > 0 {
		for _, item := range loadOfficialAutoRouteItemsFiltered(userID, officialIDs, true, false) {
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

type keyGroupRankingSnapshot struct {
	GroupID        string
	RawSuccessRate float64
	AvgTTFTMs      float64
	RequestCount   int64
}

func loadKeyGroupRankingSnapshots(groups []keyGroupOptionRow) (map[string]keyGroupRankingSnapshot, error) {
	groupIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
	}
	if len(groupIDs) == 0 {
		return map[string]keyGroupRankingSnapshot{}, nil
	}
	// These four site-level statistics contain no viewer-specific values.
	// Share only overlapping reads for the exact already-authorized ID set;
	// invitations, blocks and personal prices are resolved separately above.
	encodedIDs, _ := json.Marshal(groupIDs)
	digest := sha256.Sum256(encodedIDs)
	key := fmt.Sprintf("%p:%x", platformdb.DB, digest)
	value, err, _ := keyGroupRankingLoads.Do(key, func() (any, error) {
		var rows []keyGroupRankingSnapshot
		if err := platformdb.DB.Model(&marketplaceschema.RankingSnapshot{}).
			Select("group_id, raw_success_rate, avg_ttft_ms, request_count").
			Where("group_id IN ? AND window_hours = ? AND ranking_version = ?", groupIDs, 24, rankingVersion).
			Find(&rows).Error; err != nil {
			return nil, err
		}
		result := make(map[string]keyGroupRankingSnapshot, len(rows))
		for _, row := range rows {
			result[row.GroupID] = row
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(map[string]keyGroupRankingSnapshot), nil
}

type keyGroupOptionRow struct {
	ID                  string
	ChannelID           string
	OwnerUserID         int
	SourceType          string
	CreditPoolPolicy    string
	Multiplier          float64
	ApprovedSourceLabel string
	SourceLabelStatus   string
	DeclaredModels      string
	InternalChannelID   *int
	PersonalMultiplier  float64
}

// The API key selector searches this complete, lightweight result locally.
// Personal prices, invitations and blocks remain scoped to the current user.
func loadKeyGroupOptionRows(userID int) ([]keyGroupOptionRow, error) {
	var groups []keyGroupOptionRow
	query := GroupQuery{
		ViewerUserID:  userID,
		IncludeAccess: true,
		Verification:  marketplacedomain.VerificationPassed,
	}
	channelTable := (marketplaceschema.Channel{}).TableName()
	multiplierTable := (marketplaceschema.UserMultiplier{}).TableName()
	blockTable := (marketplaceschema.ChannelUserBlock{}).TableName()
	// Subqueries keep visibility/review conditions unambiguous while returning
	// only selector fields. No 10,000-ID lists or per-option reads are needed.
	visible := publicGroupsQuery(query).Select("id, channel_id, owner_user_id, source_type, credit_pool_policy, multiplier, lifecycle_status, updated_at")
	if err := platformdb.DB.Unscoped().Table("(?) AS visible_groups", visible).
		Select("visible_groups.id, visible_groups.channel_id, visible_groups.owner_user_id, visible_groups.source_type, visible_groups.credit_pool_policy, visible_groups.multiplier, c.approved_source_label, c.source_label_status, c.declared_models, c.internal_channel_id, COALESCE(um.multiplier, 0) AS personal_multiplier").
		Joins("JOIN "+channelTable+" AS c ON c.id = visible_groups.channel_id AND c.deleted_at IS NULL").
		Joins("LEFT JOIN "+multiplierTable+" AS um ON um.channel_id = c.id AND um.user_id = ?", userID).
		Where(fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s b WHERE b.channel_id = c.id AND b.user_id = ?)", blockTable), userID).
		Order("visible_groups.updated_at DESC, visible_groups.id ASC").
		Find(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
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
