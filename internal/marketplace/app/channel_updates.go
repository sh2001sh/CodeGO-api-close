package app

import (
	"encoding/json"
	"fmt"
	"strings"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
)

func applyChannelUpdate(channel *marketplaceschema.Channel, group *marketplaceschema.Group, req UpdateChannelRequest) (bool, error) {
	reverify, err := applyChannelModelUpdate(channel, req)
	if err != nil {
		return false, err
	}
	presentationChanged, err := applyChannelPresentationUpdate(channel, group, req)
	if err != nil {
		return false, err
	}
	reverify = reverify || presentationChanged
	if err := applyCapacityUpdate(channel, req); err != nil {
		return false, err
	}
	if err := applyAutoProbeUpdate(channel, req); err != nil {
		return false, err
	}
	if req.SensitiveWordInterceptionEnabled != nil {
		channel.SensitiveWordInterceptionEnabled = req.SensitiveWordInterceptionEnabled
	}
	changed, err := applyCredentialUpdate(channel, req)
	if err != nil {
		return false, err
	}
	if reverify || changed {
		reverify = true
		invalidateChannelVerification(channel, group)
	}
	normalizeInternalGroupName(group, channel.ID, channel.SubmittedSourceLabel)
	return reverify, nil
}

func applyChannelModelUpdate(channel *marketplaceschema.Channel, req UpdateChannelRequest) (bool, error) {
	reverify := false
	if req.ProviderType != nil {
		if err := validateProvider(*req.ProviderType); err != nil {
			return false, err
		}
		provider := strings.TrimSpace(*req.ProviderType)
		if provider != channel.ProviderType {
			channel.ProviderType = provider
			reverify = true
		}
	}
	if req.DeclaredModels != nil {
		models := normalizeModels(*req.DeclaredModels)
		if err := validateModels(models); err != nil {
			return false, err
		}
		encodedModels, _ := json.Marshal(models)
		reverify = reverify || channel.DeclaredModels != string(encodedModels)
		channel.DeclaredModels = string(encodedModels)
		prices := decodeChannelModelPrices(channel.ModelPrices)
		if req.ModelPrices != nil {
			prices = *req.ModelPrices
		}
		encodedPrices, err := encodeChannelModelPrices(retainChannelModelPrices(prices, models), models)
		if err != nil {
			return false, err
		}
		if err := validateImageModelPrices(prices, models); err != nil {
			return false, err
		}
		channel.ModelPrices = encodedPrices
		return reverify, nil
	}
	if req.ModelPrices != nil {
		prices, err := encodeChannelModelPrices(*req.ModelPrices, decodeModels(channel.DeclaredModels))
		if err != nil {
			return false, err
		}
		if err := validateImageModelPrices(*req.ModelPrices, decodeModels(channel.DeclaredModels)); err != nil {
			return false, err
		}
		channel.ModelPrices = prices
	}
	return reverify, nil
}

func applyChannelPresentationUpdate(channel *marketplaceschema.Channel, group *marketplaceschema.Group, req UpdateChannelRequest) (bool, error) {
	if req.Multiplier != nil {
		if err := applyMultiplierChange(group, channel.ID, channel.SubmittedSourceLabel, *req.Multiplier); err != nil {
			return false, err
		}
	}
	if req.Visibility != nil {
		if err := validateVisibility(*req.Visibility); err != nil {
			return false, err
		}
		group.Visibility = *req.Visibility
	}
	if req.SourceLabel != nil {
		if err := validateSourceLabel(channel.ProviderType, *req.SourceLabel); err != nil {
			return false, err
		}
		label, _ := canonicalSourceLabel(*req.SourceLabel)
		if label != channel.SubmittedSourceLabel {
			channel.SubmittedSourceLabel = label
			channel.ApprovedSourceLabel = label
			channel.SourceLabelStatus = marketplacedomain.SourceLabelApproved
			channel.SourceLabelReviewReason = ""
			refreshInternalGroupName(group, channel.ID, label)
		}
	}
	return false, nil
}

func applyAutoProbeUpdate(channel *marketplaceschema.Channel, req UpdateChannelRequest) error {
	enabled := channel.AutoProbeEnabled
	interval := channel.AutoProbeIntervalMinutes
	model := channel.AutoProbeModel
	if req.AutoProbeEnabled != nil {
		enabled = *req.AutoProbeEnabled
	}
	if req.AutoProbeIntervalMinutes != nil {
		interval = *req.AutoProbeIntervalMinutes
	}
	if req.AutoProbeModel != nil {
		model = strings.TrimSpace(*req.AutoProbeModel)
	}
	if interval == 0 {
		interval = 10
	}
	models := decodeModels(channel.DeclaredModels)
	if req.DeclaredModels != nil && enabled && !containsFold(models, model) {
		model = models[0]
	}
	if err := validateAutoProbe(enabled, interval, model, models); err != nil {
		return err
	}
	channel.AutoProbeEnabled = enabled
	channel.AutoProbeIntervalMinutes = interval
	channel.AutoProbeModel = model
	return nil
}

func invalidateChannelVerification(channel *marketplaceschema.Channel, group *marketplaceschema.Group) {
	channel.ModelVerificationResults = "[]"
	channel.ConnectivityTestStatus = ""
	channel.ConnectivityTestCheckedAt = nil
	channel.GPT56MappingResults = "[]"
	channel.GPT56MappingStatus = ""
	channel.GPT56MappingCheckedAt = nil
	channel.GPT56MappingLevel = ""
	channel.GPT56MappingTrigger = ""
	channel.Status = marketplacedomain.LifecycleDraft
	group.LifecycleStatus = marketplacedomain.LifecycleDraft
	group.VerificationStatus = marketplacedomain.VerificationQueued
	group.VerificationDueAt = nil
	group.PublishedAt = nil
}

func normalizeInternalGroupName(group *marketplaceschema.Group, channelID, sourceLabel string) {
	expected := marketplaceInternalGroupName(sourceLabel, group.ID)
	if group.InternalGroupName == expected {
		group.SystemDisplayName = marketplaceDisplayName(sourceLabel, group.Multiplier, channelID)
		return
	}
	group.RoutingVersion++
	group.InternalGroupName = expected
	group.SystemDisplayName = marketplaceDisplayName(sourceLabel, group.Multiplier, channelID)
}

func applyMultiplierChange(group *marketplaceschema.Group, channelID, sourceLabel string, multiplier float64) error {
	if err := validateMultiplier(multiplier); err != nil {
		return err
	}
	multiplier = marketplacedomain.NormalizeMultiplier(multiplier)
	if group.Multiplier == multiplier {
		return nil
	}
	group.Multiplier = multiplier
	group.RoutingVersion++
	group.InternalGroupName = marketplaceInternalGroupName(sourceLabel, group.ID)
	group.SystemDisplayName = marketplaceDisplayName(sourceLabel, multiplier, channelID)
	return nil
}

func applyCapacityUpdate(channel *marketplaceschema.Channel, req UpdateChannelRequest) error {
	if req.MaxConcurrency != nil {
		if err := validateConcurrencyLimit(*req.MaxConcurrency); err != nil {
			return fmt.Errorf("渠道总并发: %w", err)
		}
		channel.MaxConcurrency = *req.MaxConcurrency
	}
	if req.UserMaxConcurrency != nil {
		if err := validateConcurrencyLimit(*req.UserMaxConcurrency); err != nil {
			return fmt.Errorf("单用户并发: %w", err)
		}
		channel.UserMaxConcurrency = *req.UserMaxConcurrency
	}
	if req.QPS != nil && *req.QPS > 0 && *req.QPS <= 10000 {
		channel.QPS = *req.QPS
	}
	if req.MaintenanceWindow != nil {
		channel.MaintenanceWindow = strings.TrimSpace(*req.MaintenanceWindow)
	}
	return nil
}

func applyCredentialUpdate(channel *marketplaceschema.Channel, req UpdateChannelRequest) (bool, error) {
	changed := false
	if req.BaseURL != nil {
		normalized := strings.TrimRight(strings.TrimSpace(*req.BaseURL), "/")
		if err := ValidateMarketplaceURL(normalized); err != nil {
			return false, err
		}
		current, decryptErr := platformsecurity.DecryptSecret(channel.BaseURLCiphertext)
		if decryptErr != nil || strings.TrimRight(strings.TrimSpace(current), "/") != normalized {
			value, err := platformsecurity.EncryptSecret(normalized)
			if err != nil {
				return false, err
			}
			channel.BaseURLCiphertext = value
			changed = true
		}
	}
	if req.APIKey != nil && strings.TrimSpace(*req.APIKey) != "" {
		normalized := strings.TrimSpace(*req.APIKey)
		current, err := platformsecurity.DecryptSecret(channel.CredentialCiphertext)
		if err == nil && strings.TrimSpace(current) == normalized {
			return changed, nil
		}
		value, err := platformsecurity.EncryptSecret(normalized)
		if err != nil {
			return false, err
		}
		channel.CredentialCiphertext = value
		channel.CredentialTail = credentialTail(*req.APIKey)
		channel.CredentialVersion++
		changed = true
	}
	return changed, nil
}

func refreshInternalGroupName(group *marketplaceschema.Group, channelID, sourceLabel string) {
	group.RoutingVersion++
	group.InternalGroupName = marketplaceInternalGroupName(sourceLabel, group.ID)
	group.SystemDisplayName = marketplaceDisplayName(sourceLabel, group.Multiplier, channelID)
}
