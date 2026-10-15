package app

import (
	"strings"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

// UsageLogGroupOption separates the stable audit filter value from its public identity.
type UsageLogGroupOption struct {
	Value              string `json:"value"`
	Label              string `json:"label"`
	PublicID           string `json:"public_id,omitempty"`
	MarketplaceGroupID string `json:"marketplace_group_id,omitempty"`
}

// BuildUsageLogGroupOptions resolves marketplace groups without exposing internal routing names as labels.
func BuildUsageLogGroupOptions(values []string) ([]UsageLogGroupOption, error) {
	cleaned := uniqueUsageLogGroupValues(values)
	if len(cleaned) == 0 {
		return []UsageLogGroupOption{}, nil
	}

	groups, err := findUsageLogMarketplaceGroups(cleaned)
	if err != nil {
		return nil, err
	}
	channels, err := findUsageLogMarketplaceChannels(groups)
	if err != nil {
		return nil, err
	}

	groupByInternalName := make(map[string]marketplaceschema.Group, len(groups))
	for _, group := range groups {
		groupByInternalName[group.InternalGroupName] = group
	}

	options := make([]UsageLogGroupOption, 0, len(cleaned))
	for _, value := range cleaned {
		option := UsageLogGroupOption{Value: value, Label: value}
		group, exists := groupByInternalName[value]
		channel, hasChannel := channels[group.ChannelID]
		if exists && hasChannel {
			option.Label = marketplaceDisplayName(publicSourceLabel(channel), group.Multiplier, channel.ID)
			option.PublicID = channel.ID
			option.MarketplaceGroupID = group.ID
		}
		options = append(options, option)
	}
	return options, nil
}

func uniqueUsageLogGroupValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func findUsageLogMarketplaceGroups(internalNames []string) ([]marketplaceschema.Group, error) {
	groups := make([]marketplaceschema.Group, 0)
	err := platformdb.DB.Select("id", "internal_group_name", "channel_id", "multiplier").
		Where("internal_group_name IN ?", internalNames).
		Find(&groups).Error
	return groups, err
}

func findUsageLogMarketplaceChannels(groups []marketplaceschema.Group) (map[string]marketplaceschema.Channel, error) {
	channelIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		channelIDs = append(channelIDs, group.ChannelID)
	}
	channels := make([]marketplaceschema.Channel, 0, len(channelIDs))
	if len(channelIDs) > 0 {
		if err := platformdb.DB.Select("id", "approved_source_label", "source_label_status").
			Where("id IN ?", channelIDs).Find(&channels).Error; err != nil {
			return nil, err
		}
	}
	byID := make(map[string]marketplaceschema.Channel, len(channels))
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	return byID, nil
}
