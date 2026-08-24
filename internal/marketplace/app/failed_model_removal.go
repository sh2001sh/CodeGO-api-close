package app

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

// RemoveOwnerFailedChannelModel removes one failed model while retaining successful test evidence.
func RemoveOwnerFailedChannelModel(ownerUserID int, channelID, model string) (*ChannelView, error) {
	channel, group, err := loadOwnedChannelGroup(ownerUserID, channelID)
	if err != nil {
		return nil, err
	}
	return removeFailedChannelModel(channel, group, model)
}

// RemoveAdminFailedChannelModel removes one failed model from any marketplace channel.
func RemoveAdminFailedChannelModel(channelID, model string) (*ChannelView, error) {
	channel, group, err := loadChannelGroup(channelID)
	if err != nil {
		return nil, err
	}
	return removeFailedChannelModel(channel, group, model)
}

func removeFailedChannelModel(
	channel *marketplaceschema.Channel,
	group *marketplaceschema.Group,
	requestedModel string,
) (*ChannelView, error) {
	if verificationInProgress(channel) {
		return nil, errors.New("检测仍在进行，请先暂停检测再剔除模型")
	}
	declared := decodeModels(channel.DeclaredModels)
	model, ok := canonicalDeclaredModel(declared, requestedModel)
	if !ok {
		return nil, errors.New("模型不在当前渠道声明列表中")
	}
	if len(declared) <= 1 {
		return nil, errors.New("渠道至少需要保留一个模型")
	}
	results := decodeModelVerificationResults(channel.ModelVerificationResults)
	result, ok := modelVerificationResultsByModel(results)[strings.ToLower(model)]
	if !ok || result.Listed && result.Status == marketplacedomain.ModelVerificationPassed {
		return nil, errors.New("只能剔除最近一次检测中失败或上游未列出的模型")
	}

	remaining := removeModelFold(declared, model)
	retainedResults := mergeModelVerificationResults(remaining, results, nil)
	encodedModels, _ := json.Marshal(remaining)
	channel.DeclaredModels = string(encodedModels)
	channel.ModelVerificationResults = encodeModelVerificationResults(retainedResults)
	prices, err := encodeChannelModelPrices(retainChannelModelPrices(
		decodeChannelModelPrices(channel.ModelPrices), remaining,
	), remaining)
	if err != nil {
		return nil, err
	}
	channel.ModelPrices = prices
	if !containsFold(remaining, channel.AutoProbeModel) {
		channel.AutoProbeModel = remaining[0]
	}
	refreshMappingAfterModelRemoval(channel)
	applyRetainedVerificationState(channel, group, retainedResults)

	if channel.InternalChannelID == nil && group.VerificationStatus == marketplacedomain.VerificationPassed {
		if err := createInternalChannel(channel, group); err != nil {
			return nil, err
		}
	} else if channel.InternalChannelID != nil {
		if err := syncInternalChannel(channel, group); err != nil {
			return nil, err
		}
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(channel).Error; err != nil {
			return err
		}
		return tx.Save(group).Error
	}); err != nil {
		return nil, err
	}
	return channelView(channel, group), nil
}

func applyRetainedVerificationState(
	channel *marketplaceschema.Channel,
	group *marketplaceschema.Group,
	results []ModelVerificationResult,
) {
	remaining := decodeModels(channel.DeclaredModels)
	passed, rejected := selectVerifiedModels(results)
	allPassed := len(results) == len(remaining) && len(passed) == len(remaining) && len(rejected) == 0
	if !allPassed {
		channel.ConnectivityTestStatus = marketplacedomain.VerificationFailed
		channel.Status = marketplacedomain.LifecycleDraft
		group.VerificationStatus = marketplacedomain.VerificationFailed
		group.LifecycleStatus = marketplacedomain.LifecycleDraft
		group.VerificationDueAt = nil
		return
	}
	now := time.Now().UTC()
	channel.ConnectivityTestStatus = marketplacedomain.VerificationPassed
	channel.ConnectivityTestCheckedAt = &now
	verification, lifecycle := requiredVerificationState(channel)
	channel.Status = lifecycle
	group.VerificationStatus = verification
	group.LifecycleStatus = lifecycle
	if verification == marketplacedomain.VerificationPassed {
		dueAt := now.Add(7 * 24 * time.Hour)
		group.VerificationDueAt = &dueAt
		group.PublishedAt = &now
	} else {
		group.VerificationDueAt = nil
	}
}

func refreshMappingAfterModelRemoval(channel *marketplaceschema.Channel) {
	if !isGPT56MappingEligible(channel) {
		channel.GPT56MappingResults = "[]"
		channel.GPT56MappingStatus = ""
		channel.GPT56MappingCheckedAt = nil
		channel.GPT56MappingLevel = ""
		channel.GPT56MappingTrigger = ""
		return
	}
	declared := gpt56MappingModelsForChannel(channel)
	retained := make([]GPT56MappingResult, 0, len(declared))
	byModel := make(map[string]GPT56MappingResult)
	for _, result := range decodeGPT56MappingResults(channel.GPT56MappingResults) {
		byModel[strings.ToLower(strings.TrimSpace(result.RequestedModel))] = result
	}
	for _, model := range declared {
		if result, ok := byModel[strings.ToLower(model)]; ok {
			retained = append(retained, result)
		}
	}
	channel.GPT56MappingResults = encodeGPT56MappingResults(retained)
	if len(retained) == len(declared) {
		channel.GPT56MappingStatus = gpt56MappingStatus(retained)
	} else {
		channel.GPT56MappingStatus = ""
	}
}

func encodeGPT56MappingResults(results []GPT56MappingResult) string {
	encoded, err := json.Marshal(results)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func canonicalDeclaredModel(models []string, requested string) (string, bool) {
	requested = strings.TrimSpace(requested)
	for _, model := range models {
		if strings.EqualFold(model, requested) {
			return model, true
		}
	}
	return "", false
}

func removeModelFold(models []string, removed string) []string {
	remaining := make([]string, 0, len(models)-1)
	for _, model := range models {
		if !strings.EqualFold(model, removed) {
			remaining = append(remaining, model)
		}
	}
	return remaining
}
