package store

import (
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

func LoadAllEnabledAbilitiesWithChannels() ([]gatewayschema.AbilityWithChannel, error) {
	var abilities []gatewayschema.AbilityWithChannel
	err := platformdb.DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type").
		Joins("left join channels on abilities.channel_id = channels.id").
		Where("abilities.enabled = ?", true).
		Scan(&abilities).Error
	return abilities, err
}

func LoadGroupEnabledModels(group string) []string {
	var models []string
	platformdb.DB.Model(&gatewayschema.Ability{}).Where(&gatewayschema.Ability{Group: group, Enabled: true}).Distinct("model").Pluck("model", &models)
	return models
}

func LoadEnabledModels() []string {
	var models []string
	platformdb.DB.Model(&gatewayschema.Ability{}).Where(&gatewayschema.Ability{Enabled: true}).Distinct("model").Pluck("model", &models)
	return models
}

// ChannelHasExclusiveEnabledAbility reports whether disabling a channel would
// leave any group/model pair without an enabled route.
func ChannelHasExclusiveEnabledAbility(channelID int) (bool, error) {
	if channelID <= 0 {
		return false, nil
	}

	var count int64
	query := platformdb.DB.Table("abilities AS candidate").
		Where("candidate.channel_id = ? AND candidate.enabled = ?", channelID, true).
		Where(`NOT EXISTS (
			SELECT 1 FROM abilities AS alternative
			WHERE alternative."group" = candidate."group"
				AND alternative.model = candidate.model
				AND alternative.enabled = ?
				AND alternative.channel_id <> candidate.channel_id
		)`, true)
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasAlternativeEnabledAbility reports whether another enabled channel can
// serve the same group/model pair outside an automatic route pool.
func HasAlternativeEnabledAbility(channelID int, group string, modelName string) (bool, error) {
	if channelID <= 0 || group == "" || modelName == "" {
		return false, nil
	}

	models := []string{modelName}
	if normalizedModel := FormatMatchingModelName(modelName); normalizedModel != "" && normalizedModel != modelName {
		models = append(models, normalizedModel)
	}
	var count int64
	groupColumn := "`group`"
	if platformdb.UsingPostgreSQL {
		groupColumn = `"group"`
	}
	err := platformdb.DB.Model(&gatewayschema.Ability{}).
		Where(groupColumn+" = ? AND model IN ? AND enabled = ? AND channel_id <> ?", group, models, true, channelID).
		Count(&count).Error
	return count > 0, err
}

// HasAlternativeSelectableRoute reports whether the active group has another
// channel that the selector may actually use. An enabled route pool is
// authoritative: channels outside it, or disabled pool members, cannot keep a
// failing sole pool member eligible for cooldown.
func HasAlternativeSelectableRoute(channelID int, group string, modelName string) (bool, error) {
	if channelID <= 0 || group == "" || modelName == "" {
		return false, nil
	}
	detail, err := LoadEnabledRoutePool(group)
	if err != nil || detail == nil {
		return false, err
	}
	candidates, err := LoadRoutePoolCandidates(group, modelName, detail)
	if err != nil {
		return false, err
	}
	for _, candidate := range candidates {
		if candidate.Channel != nil && candidate.Channel.Id != channelID {
			return true, nil
		}
	}
	return false, nil
}

// LoadAlternativeEnabledChannels loads enabled alternatives for one group/model
// pair so the current user request can retry a cooling backup route.
func LoadAlternativeEnabledChannels(channelID int, group string, modelName string) ([]*gatewayschema.Channel, error) {
	if channelID <= 0 || group == "" || modelName == "" {
		return nil, nil
	}

	models := []string{modelName}
	if normalizedModel := FormatMatchingModelName(modelName); normalizedModel != "" && normalizedModel != modelName {
		models = append(models, normalizedModel)
	}
	groupColumn := "`group`"
	if platformdb.UsingPostgreSQL {
		groupColumn = `"group"`
	}
	var channelIDs []int
	if err := platformdb.DB.Model(&gatewayschema.Ability{}).
		Where(groupColumn+" = ? AND model IN ? AND enabled = ? AND channel_id <> ?", group, models, true, channelID).
		Distinct("channel_id").Pluck("channel_id", &channelIDs).Error; err != nil {
		return nil, err
	}
	if len(channelIDs) == 0 {
		return nil, nil
	}
	return LoadChannelsByIDs(channelIDs)
}
