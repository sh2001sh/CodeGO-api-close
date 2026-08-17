package app

import (
	"errors"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

const nativeDetectorVersion = "3.0.0"

// QueueRequiredVerification selects the publish gate required by the channel's declared models.
func QueueRequiredVerification(channelID string) error {
	channel, _, err := loadChannelGroup(channelID)
	if err != nil {
		return err
	}
	if isGPT56MappingEligible(channel) {
		return QueueGPT56MappingVerification(channelID)
	}
	return QueueConnectivityTest(channelID)
}

// QueueGPT56MappingVerification collects independent mapping evidence for GPT-5.6 models.
func QueueGPT56MappingVerification(channelID string) error {
	channel, _, err := loadChannelGroup(channelID)
	if err != nil {
		return err
	}
	if !isGPT56MappingEligible(channel) {
		return errors.New("该渠道未声明需要检测的 GPT-5.6 模型")
	}
	if err := setRequiredVerificationQueued(channelID); err != nil {
		return err
	}
	go executeGPT56MappingVerification(channelID)
	return nil
}

// QueueConnectivityTest persists a run before testing each declared model.
func QueueConnectivityTest(channelID string) error {
	run := &marketplaceschema.VerificationRun{
		ChannelID: channelID, Status: marketplacedomain.VerificationQueued,
		Stage: "basic_security", DetectorName: "NativeCompatibilityDetector",
		DetectorVersion: nativeDetectorVersion, RulesetVersion: "marketplace-v1",
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(run).Error; err != nil {
			return err
		}
		if err := tx.Model(&marketplaceschema.Channel{}).Where("id = ?", channelID).
			Updates(map[string]any{
				"connectivity_test_status":     marketplacedomain.VerificationQueued,
				"connectivity_test_checked_at": nil,
				"model_verification_results":   "[]",
			}).Error; err != nil {
			return err
		}
		channel, _, err := loadChannelGroupWithDB(tx, channelID)
		if err != nil {
			return err
		}
		if isGPT56MappingEligible(channel) {
			return nil
		}
		return setRequiredVerificationQueuedWithDB(tx, channelID)
	}); err != nil {
		return err
	}
	go executeNativeVerification(run.ID)
	return nil
}

// QueueNativeVerification is kept for internal callers that used the old name.
func QueueNativeVerification(channelID string) error {
	return QueueConnectivityTest(channelID)
}

func setRequiredVerificationQueued(channelID string) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return setRequiredVerificationQueuedWithDB(tx, channelID)
	})
}

func setRequiredVerificationQueuedWithDB(tx *gorm.DB, channelID string) error {
	if err := tx.Model(&marketplaceschema.Channel{}).Where("id = ?", channelID).
		Update("status", marketplacedomain.LifecycleVerifying).Error; err != nil {
		return err
	}
	return tx.Model(&marketplaceschema.Group{}).Where("channel_id = ?", channelID).Updates(map[string]any{
		"lifecycle_status":    marketplacedomain.LifecycleVerifying,
		"verification_status": marketplacedomain.VerificationQueued,
		"verification_due_at": nil,
	}).Error
}

func executeNativeVerification(runID string) {
	run, channel, group, err := loadVerificationContext(runID)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	_ = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(run).Updates(map[string]any{"status": marketplacedomain.VerificationRunning, "started_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(channel).Update("connectivity_test_status", marketplacedomain.VerificationRunning).Error
	})
	results, err := probeMarketplaceChannel(channel, func(stage string) {
		_ = platformdb.DB.Model(run).Update("stage", stage).Error
	}, func(results []ModelVerificationResult) {
		_ = platformdb.DB.Model(channel).Update(
			"model_verification_results", encodeModelVerificationResults(results),
		).Error
	})
	completeVerification(run, channel, group, results, err)
}

func executeGPT56MappingVerification(channelID string) {
	channel, group, err := loadChannelGroup(channelID)
	if err != nil || !isGPT56MappingEligible(channel) {
		return
	}
	_, err = runGPT56MappingCheck(channel)
	if err != nil {
		return
	}
	if err := platformdb.DB.First(channel, "id = ?", channelID).Error; err != nil {
		return
	}
	completeGPT56MappingVerification(channel, group)
}

func completeGPT56MappingVerification(channel *marketplaceschema.Channel, group *marketplaceschema.Group) {
	now := time.Now().UTC()
	lifecycle := marketplacedomain.LifecycleDraft
	verification := marketplacedomain.VerificationFailed
	if channel.GPT56MappingStatus == GPT56MappingStatusMatched {
		if channel.InternalChannelID == nil {
			if err := createInternalChannel(channel, group); err == nil {
				lifecycle = marketplacedomain.LifecycleActive
				verification = marketplacedomain.VerificationPassed
			}
		} else if err := syncInternalChannel(channel, group); err == nil {
			lifecycle = marketplacedomain.LifecycleActive
			verification = marketplacedomain.VerificationPassed
		}
	}
	_ = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(channel).Update("status", lifecycle).Error; err != nil {
			return err
		}
		updates := map[string]any{"lifecycle_status": lifecycle, "verification_status": verification}
		if verification == marketplacedomain.VerificationPassed {
			updates["verification_due_at"] = now.Add(7 * 24 * time.Hour)
			updates["published_at"] = now
		}
		return tx.Model(group).Updates(updates).Error
	})
}

func loadChannelGroupWithDB(db *gorm.DB, channelID string) (*marketplaceschema.Channel, *marketplaceschema.Group, error) {
	var channel marketplaceschema.Channel
	if err := db.First(&channel, "id = ?", channelID).Error; err != nil {
		return nil, nil, err
	}
	var group marketplaceschema.Group
	if err := db.First(&group, "channel_id = ?", channelID).Error; err != nil {
		return nil, nil, err
	}
	return &channel, &group, nil
}

func loadVerificationContext(runID string) (*marketplaceschema.VerificationRun, *marketplaceschema.Channel, *marketplaceschema.Group, error) {
	var run marketplaceschema.VerificationRun
	if err := platformdb.DB.First(&run, "id = ?", runID).Error; err != nil {
		return nil, nil, nil, err
	}
	var channel marketplaceschema.Channel
	if err := platformdb.DB.First(&channel, "id = ?", run.ChannelID).Error; err != nil {
		return nil, nil, nil, err
	}
	var group marketplaceschema.Group
	if err := platformdb.DB.First(&group, "channel_id = ?", channel.ID).Error; err != nil {
		return nil, nil, nil, err
	}
	return &run, &channel, &group, nil
}
