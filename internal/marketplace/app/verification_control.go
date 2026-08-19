package app

import (
	"errors"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

var errVerificationNotRunning = errors.New("检测任务已暂停或结束")

// PauseOwnerChannelVerification pauses an owner's active channel verification.
func PauseOwnerChannelVerification(ownerUserID int, channelID string) error {
	var channel marketplaceschema.Channel
	if err := platformdb.DB.Where("id = ? AND owner_user_id = ?", channelID, ownerUserID).
		First(&channel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("渠道不存在或无权限")
		}
		return err
	}
	return PauseChannelVerification(channelID)
}

// PauseChannelVerification cancels active probes and preserves partial evidence.
func PauseChannelVerification(channelID string) error {
	channel, _, err := loadChannelGroup(channelID)
	if err != nil {
		return err
	}
	if !verificationInProgress(channel) {
		return errors.New("当前没有正在进行的检测")
	}
	marketplaceVerificationTasks.cancelChannel(channelID)
	return pauseChannelVerificationState(channel)
}

func verificationInProgress(channel *marketplaceschema.Channel) bool {
	return channel != nil && (statusInProgress(channel.ConnectivityTestStatus) ||
		mappingStatusInProgress(channel.GPT56MappingStatus))
}

func statusInProgress(status string) bool {
	return status == marketplacedomain.VerificationQueued ||
		status == marketplacedomain.VerificationRunning
}

func mappingStatusInProgress(status string) bool {
	return status == GPT56MappingStatusQueued || status == GPT56MappingStatusRunning
}

func pauseChannelVerificationState(channel *marketplaceschema.Channel) error {
	now := time.Now().UTC()
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&marketplaceschema.VerificationRun{}).
			Where("channel_id = ? AND status IN ?", channel.ID, []string{
				marketplacedomain.VerificationQueued, marketplacedomain.VerificationRunning,
			}).Updates(map[string]any{
			"status": marketplacedomain.VerificationPaused, "completed_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&marketplaceschema.GPT56MappingRun{}).
			Where("channel_id = ? AND status IN ?", channel.ID, []string{
				GPT56MappingStatusQueued, GPT56MappingStatusRunning,
			}).Updates(map[string]any{
			"status": GPT56MappingStatusPaused, "completed_at": now,
		}).Error; err != nil {
			return err
		}
		channelUpdates := map[string]any{"status": marketplacedomain.LifecycleDraft}
		if statusInProgress(channel.ConnectivityTestStatus) {
			channelUpdates["connectivity_test_status"] = marketplacedomain.VerificationPaused
		}
		if mappingStatusInProgress(channel.GPT56MappingStatus) {
			channelUpdates["gpt56_mapping_status"] = GPT56MappingStatusPaused
		}
		if err := tx.Model(channel).Updates(channelUpdates).Error; err != nil {
			return err
		}
		return tx.Model(&marketplaceschema.Group{}).Where("channel_id = ?", channel.ID).
			Updates(map[string]any{
				"lifecycle_status":    marketplacedomain.LifecycleDraft,
				"verification_status": marketplacedomain.VerificationPaused,
				"verification_due_at": nil,
			}).Error
	})
}

func reconcileInterruptedVerifications() error {
	var channels []marketplaceschema.Channel
	if err := platformdb.DB.Where(
		"connectivity_test_status IN ? OR gpt56_mapping_status IN ?",
		[]string{marketplacedomain.VerificationQueued, marketplacedomain.VerificationRunning},
		[]string{GPT56MappingStatusQueued, GPT56MappingStatusRunning},
	).Find(&channels).Error; err != nil {
		return err
	}
	for index := range channels {
		if err := pauseChannelVerificationState(&channels[index]); err != nil {
			return err
		}
	}
	return nil
}
