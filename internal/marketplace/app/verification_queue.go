package app

import (
	"context"
	"errors"
	"time"

	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
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
	if len(verifiableMarketplaceModels(decodeModels(channel.DeclaredModels))) == 0 {
		return publishImageOnlyChannel(channel)
	}
	return QueueIncrementalConnectivityTest(channelID)
}

func verifiableMarketplaceModels(models []string) []string {
	result := make([]string, 0, len(models))
	for _, model := range normalizeModels(models) {
		if !gatewaycontract.IsImageGenerationModel(model) {
			result = append(result, model)
		}
	}
	return result
}

func publishImageOnlyChannel(channel *marketplaceschema.Channel) error {
	_, group, err := loadChannelGroup(channel.ID)
	if err != nil {
		return err
	}
	if channel.InternalChannelID == nil {
		err = createInternalChannel(channel, group)
	} else {
		err = syncInternalChannel(channel, group)
	}
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(channel).Updates(map[string]any{
			"status":                       marketplacedomain.LifecycleActive,
			"connectivity_test_status":     marketplacedomain.VerificationPassed,
			"connectivity_test_checked_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(group).Updates(map[string]any{
			"lifecycle_status":    marketplacedomain.LifecycleActive,
			"verification_status": marketplacedomain.VerificationPassed,
			"verification_due_at": now.Add(7 * 24 * time.Hour),
			"published_at":        now,
		}).Error
	})
}

// QueueConnectivityTest persists a run before testing each declared model.
func QueueConnectivityTest(channelID string) error {
	channel, _, err := loadChannelGroup(channelID)
	if err != nil {
		return err
	}
	return queueConnectivityTest(channel, verifiableMarketplaceModels(decodeModels(channel.DeclaredModels)), nil)
}

// QueueIncrementalConnectivityTest probes only models without a persisted
// result. This is the path used after channel edits.
func QueueIncrementalConnectivityTest(channelID string) error {
	channel, _, err := loadChannelGroup(channelID)
	if err != nil {
		return err
	}
	declared := verifiableMarketplaceModels(decodeModels(channel.DeclaredModels))
	previous := decodeModelVerificationResults(channel.ModelVerificationResults)
	pending := pendingModelVerificationModels(declared, previous)
	if len(pending) == 0 {
		if allModelsVerified(declared, previous) {
			return publishVerifiedConnectivity(channel)
		}
		if err := persistUnchangedVerificationFailure(channel); err != nil {
			return err
		}
		return errors.New("已有失败模型，请使用“重试失败模型”")
	}
	retained := retainModelVerificationResults(declared, previous, pending)
	return queueConnectivityTest(channel, pending, retained)
}

func persistUnchangedVerificationFailure(channel *marketplaceschema.Channel) error {
	_, group, err := loadChannelGroup(channel.ID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(channel).Updates(map[string]any{
			"status":                       marketplacedomain.LifecycleDraft,
			"connectivity_test_status":     marketplacedomain.VerificationFailed,
			"connectivity_test_checked_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(group).Updates(map[string]any{
			"lifecycle_status":    marketplacedomain.LifecycleDraft,
			"verification_status": marketplacedomain.VerificationFailed,
			"verification_due_at": nil,
		}).Error
	})
}

func publishVerifiedConnectivity(channel *marketplaceschema.Channel) error {
	_, group, err := loadChannelGroup(channel.ID)
	if err != nil {
		return err
	}
	if channel.InternalChannelID == nil {
		if err := createInternalChannel(channel, group); err != nil {
			return err
		}
	} else if err := syncInternalChannel(channel, group); err != nil {
		return err
	}
	now := time.Now().UTC()
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(channel).Updates(map[string]any{
			"status":                       marketplacedomain.LifecycleActive,
			"connectivity_test_status":     marketplacedomain.VerificationPassed,
			"connectivity_test_checked_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(group).Updates(map[string]any{
			"lifecycle_status":    marketplacedomain.LifecycleActive,
			"verification_status": marketplacedomain.VerificationPassed,
			"verification_due_at": now.Add(7 * 24 * time.Hour),
			"published_at":        now,
		}).Error
	})
}

// QueueFailedConnectivityTests retries only models that failed the latest run.
func QueueFailedConnectivityTests(channelID string) error {
	channel, _, err := loadChannelGroup(channelID)
	if err != nil {
		return err
	}
	declared := verifiableMarketplaceModels(decodeModels(channel.DeclaredModels))
	previous := decodeModelVerificationResults(channel.ModelVerificationResults)
	failed := retryableConnectivityModels(declared, previous, channel.ConnectivityTestStatus)
	if len(failed) == 0 {
		return errors.New("没有可重试的失败模型")
	}
	retained := retainModelVerificationResults(decodeModels(channel.DeclaredModels), previous, failed)
	return queueConnectivityTest(channel, failed, retained)
}

func queueConnectivityTest(channel *marketplaceschema.Channel, models []string, retained []ModelVerificationResult) error {
	ctx, finish, started := marketplaceVerificationTasks.begin(
		context.Background(), channel.ID, verificationTaskConnectivity,
	)
	if !started {
		return errors.New("模型连通性测试正在进行")
	}
	run := &marketplaceschema.VerificationRun{
		ChannelID: channel.ID, Status: marketplacedomain.VerificationQueued,
		Stage: "basic_security", DetectorName: "NativeCompatibilityDetector",
		DetectorVersion: nativeDetectorVersion, RulesetVersion: "marketplace-v1",
	}
	if err := prepareConnectivityTest(run, retained); err != nil {
		finish()
		return err
	}
	go func() {
		defer finish()
		executeNativeVerification(ctx, run.ID, models, retained)
	}()
	return nil
}

func prepareConnectivityTest(run *marketplaceschema.VerificationRun, retained []ModelVerificationResult) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		if err := tx.Model(&marketplaceschema.VerificationRun{}).
			Where("channel_id = ? AND status IN ?", run.ChannelID, []string{
				marketplacedomain.VerificationQueued,
				marketplacedomain.VerificationRunning,
			}).Updates(map[string]any{
			"status": marketplacedomain.VerificationPaused, "completed_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(run).Error; err != nil {
			return err
		}
		if err := tx.Model(&marketplaceschema.Channel{}).Where("id = ?", run.ChannelID).
			Updates(map[string]any{
				"connectivity_test_status":     marketplacedomain.VerificationQueued,
				"connectivity_test_checked_at": nil,
				"model_verification_results":   encodeModelVerificationResults(retained),
			}).Error; err != nil {
			return err
		}
		return setRequiredVerificationQueuedWithDB(tx, run.ChannelID)
	})
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

func executeNativeVerification(ctx context.Context, runID string, models []string, retained []ModelVerificationResult) {
	run, channel, group, err := loadVerificationContext(runID)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(run).Where("status = ?", marketplacedomain.VerificationQueued).
			Updates(map[string]any{"status": marketplacedomain.VerificationRunning, "started_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errVerificationNotRunning
		}
		result = tx.Model(channel).
			Where("connectivity_test_status = ?", marketplacedomain.VerificationQueued).
			Update("connectivity_test_status", marketplacedomain.VerificationRunning)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errVerificationNotRunning
		}
		return nil
	}); err != nil {
		return
	}
	results, err := probeMarketplaceChannelModels(ctx, channel, models, func(stage string) {
		_ = platformdb.DB.Model(run).
			Where("status = ?", marketplacedomain.VerificationRunning).
			Update("stage", stage).Error
	}, func(results []ModelVerificationResult) {
		merged := mergeModelVerificationResults(decodeModels(channel.DeclaredModels), retained, results)
		_ = platformdb.DB.Model(channel).
			Where("connectivity_test_status = ?", marketplacedomain.VerificationRunning).
			Update("model_verification_results", encodeModelVerificationResults(merged)).Error
	})
	merged := mergeModelVerificationResults(decodeModels(channel.DeclaredModels), retained, results)
	completeVerification(run, channel, group, merged, err)
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
