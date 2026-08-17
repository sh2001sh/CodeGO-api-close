package app

import (
	"encoding/json"
	"strings"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

const (
	logMarketplaceGroupIDKey          = "marketplace_group_id"
	logMarketplaceGroupDisplayNameKey = "marketplace_group_display_name"
	logMarketplaceChannelIDKey        = "marketplace_channel_id"
	logMarketplacePublicSlugKey       = "marketplace_public_slug"
)

// EnrichUsageLogMarketplaceIdentity adds public marketplace identifiers while
// preserving the internal routing group stored on the audit log itself.
func EnrichUsageLogMarketplaceIdentity(logs []*auditschema.Log) error {
	groupIDs, parsed := collectUsageLogMarketplaceGroups(logs)
	if len(groupIDs) == 0 {
		return nil
	}

	var groups []marketplaceschema.Group
	if err := platformdb.DB.Select("id", "channel_id", "system_display_name", "public_slug", "multiplier").
		Where("id IN ?", groupIDs).Find(&groups).Error; err != nil {
		return err
	}
	channelIDs := make([]string, 0, len(groups))
	groupByID := make(map[string]marketplaceschema.Group, len(groups))
	for _, group := range groups {
		groupByID[group.ID] = group
		channelIDs = append(channelIDs, group.ChannelID)
	}

	channelByID := make(map[string]marketplaceschema.Channel, len(channelIDs))
	if len(channelIDs) > 0 {
		var channels []marketplaceschema.Channel
		if err := platformdb.DB.Select("id", "approved_source_label", "source_label_status").
			Where("id IN ?", channelIDs).Find(&channels).Error; err != nil {
			return err
		}
		for _, channel := range channels {
			channelByID[channel.ID] = channel
		}
	}

	for index, log := range logs {
		if log == nil || parsed[index] == nil {
			continue
		}
		groupID, _ := parsed[index][logMarketplaceGroupIDKey].(string)
		group, exists := groupByID[strings.TrimSpace(groupID)]
		if !exists {
			continue
		}
		displayName := group.SystemDisplayName
		if channel, exists := channelByID[group.ChannelID]; exists {
			displayName = marketplaceDisplayName(publicSourceLabel(channel), group.Multiplier, channel.ID)
		}
		parsed[index][logMarketplaceGroupDisplayNameKey] = displayName
		parsed[index][logMarketplaceChannelIDKey] = group.ChannelID
		parsed[index][logMarketplacePublicSlugKey] = group.PublicSlug
		encoded, err := json.Marshal(parsed[index])
		if err != nil {
			return err
		}
		log.Other = string(encoded)
	}
	return nil
}

func collectUsageLogMarketplaceGroups(logs []*auditschema.Log) ([]string, []map[string]interface{}) {
	parsed := make([]map[string]interface{}, len(logs))
	seen := make(map[string]struct{})
	groupIDs := make([]string, 0)
	for index, log := range logs {
		if log == nil || strings.TrimSpace(log.Other) == "" {
			continue
		}
		var other map[string]interface{}
		if json.Unmarshal([]byte(log.Other), &other) != nil {
			continue
		}
		parsed[index] = other
		groupID, _ := other[logMarketplaceGroupIDKey].(string)
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			continue
		}
		if _, exists := seen[groupID]; exists {
			continue
		}
		seen[groupID] = struct{}{}
		groupIDs = append(groupIDs, groupID)
	}
	return groupIDs, parsed
}
