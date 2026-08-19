package app

import (
	"context"
	"errors"
	"sync"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"gorm.io/gorm"
)

var gpt56MappingLocks sync.Map

type gpt56CheckRequest struct {
	Level       string
	Trigger     string
	ParentRunID string
}

func runGPT56MappingCheckWithRequest(
	ctx context.Context,
	channel *marketplaceschema.Channel,
	request gpt56CheckRequest,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return true, err
	}
	if !isGPT56MappingEligible(channel) {
		return false, nil
	}
	lockValue, _ := gpt56MappingLocks.LoadOrStore(channel.ID, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if err := ctx.Err(); err != nil {
		return true, err
	}

	var current marketplaceschema.Channel
	if err := platformdb.DB.First(&current, "id = ?", channel.ID).Error; err != nil {
		return true, err
	}
	if current.GPT56MappingStatus == GPT56MappingStatusRunning {
		return true, nil
	}
	return true, executeGPT56MappingCheck(ctx, &current, request)
}

func executeGPT56MappingCheck(
	ctx context.Context,
	channel *marketplaceschema.Channel,
	request gpt56CheckRequest,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	policy := gpt56Policy(request.Level)
	run, err := startGPT56MappingRun(
		channel.ID, policy.Level, request.Trigger, request.ParentRunID,
	)
	if err != nil {
		return err
	}
	models := gpt56MappingModelsForChannel(channel)
	baseURL, credential, err := gpt56MappingCredentials(channel)
	if err != nil {
		results := unavailableGPT56MappingResults(models, policy)
		return finishGPT56MappingRun(run, results, GPT56MappingStatusInsufficientEvidence)
	}
	results, err := probeGPT56MappingsWithProgressContext(
		ctx,
		channel.ProviderType, baseURL, credential, models, policy,
		func(progress []GPT56MappingResult) error {
			return saveGPT56MappingProgress(run.ID, channel.ID, progress)
		},
	)
	if err != nil {
		finishErr := finishGPT56MappingRun(
			run, results, GPT56MappingStatusInsufficientEvidence,
		)
		return errors.Join(err, finishErr)
	}
	status := gpt56MappingStatus(results)
	if err := finishGPT56MappingRun(run, results, status); err != nil {
		return err
	}
	if shouldConfirmGPT56Mapping(policy, status) {
		return executeGPT56MappingCheck(ctx, channel, gpt56CheckRequest{
			Level: GPT56MappingLevelConfirmation, Trigger: GPT56MappingTriggerConfirmation,
			ParentRunID: run.ID,
		})
	}
	if policy.Level == GPT56MappingLevelConfirmation &&
		request.Trigger == GPT56MappingTriggerConfirmation &&
		status == GPT56MappingStatusMismatch {
		return applyConfirmedGPT56Mismatch(channel.ID)
	}
	return nil
}

func gpt56MappingCredentials(channel *marketplaceschema.Channel) (string, string, error) {
	baseURL, err := platformsecurity.DecryptSecret(channel.BaseURLCiphertext)
	if err != nil {
		return "", "", err
	}
	credential, err := platformsecurity.DecryptSecret(channel.CredentialCiphertext)
	if err != nil {
		return "", "", err
	}
	return baseURL, credential, nil
}

func unavailableGPT56MappingResults(
	models []string,
	policy gpt56DetectionPolicy,
) []GPT56MappingResult {
	results := make([]GPT56MappingResult, 0, len(models))
	for _, model := range models {
		results = append(results, GPT56MappingResult{
			RequestedModel: model, Status: GPT56MappingStatusInsufficientEvidence,
			SampleCount: policy.sampleCount(), Error: "渠道凭据不可用",
			TestedAt: time.Now().UTC(),
		})
	}
	return results
}

func applyConfirmedGPT56Mismatch(channelID string) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&marketplaceschema.Channel{}).Where("id = ?", channelID).
			Update("status", marketplacedomain.LifecycleDraft).Error; err != nil {
			return err
		}
		result := tx.Model(&marketplaceschema.Group{}).Where("channel_id = ?", channelID).Updates(map[string]any{
			"lifecycle_status":    marketplacedomain.LifecycleDraft,
			"verification_status": marketplacedomain.VerificationFailed,
			"verification_due_at": nil,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("未找到检测渠道对应的市场分组")
		}
		return nil
	})
}
