package app

import (
	"strings"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

// ReconcileMarketplaceChannels upgrades legacy names and publishes verified channels.
func ReconcileMarketplaceChannels() error {
	if platformdb.DB == nil || !platformdb.DB.Migrator().HasTable(&marketplaceschema.Channel{}) {
		return nil
	}
	var channels []marketplaceschema.Channel
	if err := platformdb.DB.Find(&channels).Error; err != nil {
		return err
	}
	for index := range channels {
		channel := &channels[index]
		var group marketplaceschema.Group
		if err := platformdb.DB.First(&group, "channel_id = ?", channel.ID).Error; err != nil {
			return err
		}
		label := reconciledSourceLabel(channel.ProviderType, channel.SubmittedSourceLabel)
		channel.SubmittedSourceLabel = label
		channel.ApprovedSourceLabel = label
		channel.SourceLabelStatus = marketplacedomain.SourceLabelApproved
		channel.SourceLabelReviewReason = ""
		normalizeInternalGroupName(&group, label)

		if group.VerificationStatus == marketplacedomain.VerificationPassed &&
			group.LifecycleStatus != marketplacedomain.LifecycleSuspended &&
			group.LifecycleStatus != marketplacedomain.LifecycleDisabled {
			if channel.InternalChannelID == nil {
				if err := createInternalChannel(channel, &group); err != nil {
					return err
				}
			} else if err := syncInternalChannel(channel, &group); err != nil {
				return err
			}
			now := time.Now().UTC()
			channel.Status = marketplacedomain.LifecycleActive
			group.LifecycleStatus = marketplacedomain.LifecycleActive
			if group.PublishedAt == nil {
				group.PublishedAt = &now
			}
		}
		if err := platformdb.DB.Save(channel).Error; err != nil {
			return err
		}
		if err := platformdb.DB.Save(&group).Error; err != nil {
			return err
		}
		upgrade, err := needsVerificationUpgrade(channel.ID)
		if err != nil {
			return err
		}
		if upgrade {
			channel.Status = marketplacedomain.LifecycleVerifying
			group.LifecycleStatus = marketplacedomain.LifecycleVerifying
			group.VerificationStatus = marketplacedomain.VerificationQueued
			group.VerificationDueAt = nil
			if err := platformdb.DB.Save(channel).Error; err != nil {
				return err
			}
			if err := platformdb.DB.Save(&group).Error; err != nil {
				return err
			}
			if err := QueueNativeVerification(channel.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func needsVerificationUpgrade(channelID string) (bool, error) {
	var latest marketplaceschema.VerificationRun
	err := platformdb.DB.Where("channel_id = ?", channelID).Order("created_at desc").First(&latest).Error
	if err != nil {
		if isNotFound(err) {
			return true, nil
		}
		return false, err
	}
	return latest.DetectorVersion != nativeDetectorVersion, nil
}

func reconciledSourceLabel(provider, value string) string {
	if label, ok := canonicalSourceLabel(value); ok {
		return label
	}
	switch strings.TrimSpace(provider) {
	case "codex":
		return "Codex Plus"
	case "anthropic":
		return "CC其它"
	default:
		return "国产模型"
	}
}
