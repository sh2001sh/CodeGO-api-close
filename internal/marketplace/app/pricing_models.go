package app

import (
	"sort"
	"strings"

	"github.com/sh2001sh/new-api/constant"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

// ListAvailablePricingModels exposes names supported by visible marketplace
// groups, without loading rankings, request statistics or private credentials.
func ListAvailablePricingModels(userID int) ([]string, error) {
	groups, err := loadPublicGroupRows(GroupQuery{ViewerUserID: userID, IncludeAccess: userID > 0, Verification: marketplacedomain.VerificationPassed})
	if err != nil {
		return nil, err
	}
	blocked := map[string]struct{}{}
	if userID > 0 {
		blocked, err = loadBlockedChannelIDs(userID, groups)
		if err != nil {
			return nil, err
		}
	}
	byChannel := make(map[string]marketplaceschema.Group)
	ids := make([]string, 0, len(groups))
	for _, group := range groups {
		if _, denied := blocked[group.ChannelID]; denied ||
			(group.LifecycleStatus != marketplacedomain.LifecycleActive && group.LifecycleStatus != marketplacedomain.LifecycleDegraded) {
			continue
		}
		ids = append(ids, group.ChannelID)
		byChannel[group.ChannelID] = group
	}
	result := []string{}
	if len(ids) == 0 {
		return result, nil
	}
	var channels []marketplaceschema.Channel
	if err := platformdb.DB.Select("id, internal_channel_id, declared_models").Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return nil, err
	}
	type memberModels struct {
		group  string
		models map[string]bool
	}
	eligible := make(map[int]memberModels, len(channels))
	internalIDs := make([]int, 0, len(channels))
	for _, channel := range channels {
		if channel.InternalChannelID == nil {
			continue
		}
		models := map[string]bool{}
		for _, name := range decodeModels(channel.DeclaredModels) {
			models[strings.ToLower(strings.TrimSpace(name))] = true
		}
		eligible[*channel.InternalChannelID] = memberModels{byChannel[channel.ID].InternalGroupName, models}
		internalIDs = append(internalIDs, *channel.InternalChannelID)
	}
	if len(internalIDs) == 0 {
		return result, nil
	}
	var abilities []struct {
		ChannelID int
		Group     string
		Model     string
	}
	groupColumn := "a.`group`"
	if platformdb.UsingPostgreSQL {
		groupColumn = `a."group"`
	}
	if err := platformdb.DB.Table("abilities a").Select("a.channel_id, a.model, "+groupColumn).
		Joins("JOIN channels c ON c.id = a.channel_id").
		Where("a.enabled = ? AND c.status = ? AND a.channel_id IN ?", true, constant.ChannelStatusEnabled, internalIDs).
		Scan(&abilities).Error; err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, ability := range abilities {
		member := eligible[ability.ChannelID]
		name := strings.TrimSpace(ability.Model)
		key := strings.ToLower(name)
		if name != "" && ability.Group == member.group && member.models[key] && !seen[key] {
			seen[key] = true
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result, nil
}
