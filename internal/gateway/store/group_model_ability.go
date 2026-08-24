package store

import (
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

// HasEnabledChannelForGroupModel reports whether at least one enabled ability
// can serve the group/model pair without selecting or reserving a channel.
func HasEnabledChannelForGroupModel(group string, modelName string) bool {
	if group == "" || modelName == "" {
		return false
	}
	if !platformconfig.MemoryCacheEnabled {
		return hasEnabledChannelForGroupModelDB(group, modelName)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	if group2model2channels == nil {
		return false
	}
	if len(group2model2channels[group][modelName]) > 0 {
		return true
	}
	normalizedModel := FormatMatchingModelName(modelName)
	return normalizedModel != "" && normalizedModel != modelName &&
		len(group2model2channels[group][normalizedModel]) > 0
}

func hasEnabledChannelForGroupModelDB(group string, modelName string) bool {
	groupColumn := "`group`"
	if platformdb.UsingPostgreSQL {
		groupColumn = `"group"`
	}
	models := []string{modelName}
	if normalizedModel := FormatMatchingModelName(modelName); normalizedModel != "" && normalizedModel != modelName {
		models = append(models, normalizedModel)
	}
	var count int64
	err := platformdb.DB.Model(&gatewayschema.Ability{}).
		Where(groupColumn+" = ? and model IN ? and enabled = ?", group, models, true).
		Count(&count).Error
	return err == nil && count > 0
}
