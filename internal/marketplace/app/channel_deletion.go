package app

import (
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DeleteOwnerChannel removes a marketplace channel owned by the current user.
func DeleteOwnerChannel(ownerUserID int, channelID string) error {
	channel, group, err := loadOwnedChannelGroup(ownerUserID, channelID)
	if err != nil {
		return err
	}
	return deleteMarketplaceChannel(channel, group)
}

// DeleteAdminChannel removes any marketplace channel selected by an administrator.
func DeleteAdminChannel(channelID string) error {
	channel, group, err := loadChannelGroup(channelID)
	if err != nil {
		return err
	}
	return deleteMarketplaceChannel(channel, group)
}

func deleteMarketplaceChannel(channel *marketplaceschema.Channel, group *marketplaceschema.Group) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var current marketplaceschema.Channel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", channel.ID).Error; err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", group.ID).Delete(&marketplaceschema.AutoRoutePoolMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", group.ID).Delete(&marketplaceschema.RankingSnapshot{}).Error; err != nil {
			return err
		}
		if current.InternalChannelID != nil {
			if err := tx.Where("channel_id = ?", *current.InternalChannelID).Delete(&gatewayschema.Ability{}).Error; err != nil {
				return err
			}
			if err := tx.Delete(&gatewayschema.Channel{}, *current.InternalChannelID).Error; err != nil {
				return err
			}
		}
		if err := tx.Delete(group).Error; err != nil {
			return err
		}
		return tx.Delete(channel).Error
	})
}
