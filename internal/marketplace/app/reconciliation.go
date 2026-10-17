package app

import (
	"strings"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReconcileMarketplaceChannels upgrades legacy names and publishes verified channels.
func ReconcileMarketplaceChannels() error {
	if platformdb.DB == nil || !platformdb.DB.Migrator().HasTable(&marketplaceschema.Channel{}) {
		return nil
	}
	if err := reconcileInterruptedVerifications(); err != nil {
		return err
	}
	var channels []marketplaceschema.Channel
	if err := platformdb.DB.Find(&channels).Error; err != nil {
		return err
	}
	for index := range channels {
		channel := &channels[index]
		var group marketplaceschema.Group
		if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(channel, "id = ?", channel.ID).Error; err != nil {
				return err
			}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, "channel_id = ?", channel.ID).Error; err != nil {
				return err
			}
			if marketplaceChannelAdminHeld(channel) || group.LifecycleStatus == marketplacedomain.LifecycleDisabled {
				channel.Status, group.LifecycleStatus = marketplacedomain.LifecycleDisabled, marketplacedomain.LifecycleDisabled
				if err := tx.Save(channel).Error; err != nil {
					return err
				}
				if err := tx.Save(&group).Error; err != nil {
					return err
				}
				return syncPausedMarketplaceChannelWithDB(tx, channel, true)
			}
			if channel.Status == marketplacedomain.LifecycleSuspended || group.LifecycleStatus == marketplacedomain.LifecycleSuspended {
				return syncPausedMarketplaceChannelWithDB(tx, channel, true)
			}
			label := reconciledSourceLabel(channel.ProviderType, channel.SubmittedSourceLabel)
			channel.SubmittedSourceLabel = label
			// Startup reconciliation is not an administrator source review.
			if channel.SourceLabelStatus == "" {
				channel.SourceLabelStatus = marketplacedomain.SourceLabelPending
			}
			normalizeInternalGroupName(&group, channel.ID, label)
			if err := backfillConnectivityTestStatus(tx, channel); err != nil {
				return err
			}
			if group.VerificationStatus == marketplacedomain.VerificationPassed {
				if channel.InternalChannelID == nil {
					if err := createInternalChannelWithDB(tx, channel, &group); err != nil {
						return err
					}
				} else if err := syncInternalChannelWithDB(tx, channel, &group); err != nil {
					return err
				}
				// Publication owns lifecycle; configuration sync deliberately keeps
				// the previous internal status, including an obsolete pause.
				if err := syncPausedMarketplaceChannelWithDB(tx, channel, false); err != nil {
					return err
				}
				now := time.Now().UTC()
				channel.Status, group.LifecycleStatus = marketplacedomain.LifecycleActive, marketplacedomain.LifecycleActive
				if group.PublishedAt == nil {
					group.PublishedAt = &now
				}
			}
			if err := tx.Save(channel).Error; err != nil {
				return err
			}
			return tx.Save(&group).Error
		}); err != nil {
			return err
		}
		if channel.InternalChannelID != nil {
			refreshInternalChannelRuntime(*channel.InternalChannelID)
		}
		if marketplaceChannelAdminHeld(channel) || channel.Status == marketplacedomain.LifecycleSuspended {
			continue
		}
		upgrade, err := needsVerificationUpgrade(channel)
		if err != nil {
			return err
		}
		if upgrade {
			if err := QueueRequiredVerification(channel.ID); err != nil {
				return err
			}
		}
	}

	return nil
}

func backfillConnectivityTestStatus(db *gorm.DB, channel *marketplaceschema.Channel) error {
	if channel.ConnectivityTestStatus != "" {
		return nil
	}
	var latest marketplaceschema.VerificationRun
	err := db.Where("channel_id = ?", channel.ID).Order("created_at desc").First(&latest).Error
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	if latest.Status != marketplacedomain.VerificationPassed && latest.Status != marketplacedomain.VerificationFailed {
		return nil
	}
	channel.ConnectivityTestStatus = latest.Status
	channel.ConnectivityTestCheckedAt = latest.CompletedAt
	return nil
}

func needsVerificationUpgrade(channel *marketplaceschema.Channel) (bool, error) {
	var latest marketplaceschema.VerificationRun
	err := platformdb.DB.Where("channel_id = ?", channel.ID).Order("created_at desc").First(&latest).Error
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
