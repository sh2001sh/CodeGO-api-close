package app

import (
	"encoding/json"
	"strings"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
)

func applyChannelUpdate(channel *marketplaceschema.Channel, group *marketplaceschema.Group, req UpdateChannelRequest) (bool, error) {
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
		if err := validateModels(*req.DeclaredModels); err != nil {
			return false, err
		}
		models, _ := json.Marshal(normalizeModels(*req.DeclaredModels))
		channel.DeclaredModels = string(models)
		reverify = true
	}
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
	applyCapacityUpdate(channel, req)
	if req.SensitiveWordInterceptionEnabled != nil {
		channel.SensitiveWordInterceptionEnabled = req.SensitiveWordInterceptionEnabled
	}
	changed, err := applyCredentialUpdate(channel, req)
	if err != nil {
		return false, err
	}
	if changed {
		reverify = true
	}
	normalizeInternalGroupName(group, channel.ID, channel.SubmittedSourceLabel)
	if reverify {
		group.LifecycleStatus = marketplacedomain.LifecycleVerifying
		group.VerificationStatus = marketplacedomain.VerificationQueued
		group.VerificationDueAt = nil
	}
	return reverify, nil
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
	if group.Multiplier == multiplier {
		return nil
	}
	group.Multiplier = multiplier
	group.RoutingVersion++
	group.InternalGroupName = marketplaceInternalGroupName(sourceLabel, group.ID)
	group.SystemDisplayName = marketplaceDisplayName(sourceLabel, multiplier, channelID)
	return nil
}

func applyCapacityUpdate(channel *marketplaceschema.Channel, req UpdateChannelRequest) {
	if req.MaxConcurrency != nil && *req.MaxConcurrency > 0 && *req.MaxConcurrency <= 10000 {
		channel.MaxConcurrency = *req.MaxConcurrency
	}
	if req.QPS != nil && *req.QPS > 0 && *req.QPS <= 10000 {
		channel.QPS = *req.QPS
	}
	if req.MaintenanceWindow != nil {
		channel.MaintenanceWindow = strings.TrimSpace(*req.MaintenanceWindow)
	}
}

func applyCredentialUpdate(channel *marketplaceschema.Channel, req UpdateChannelRequest) (bool, error) {
	changed := false
	if req.BaseURL != nil {
		if err := ValidateMarketplaceURL(*req.BaseURL); err != nil {
			return false, err
		}
		value, err := platformsecurity.EncryptSecret(strings.TrimRight(strings.TrimSpace(*req.BaseURL), "/"))
		if err != nil {
			return false, err
		}
		channel.BaseURLCiphertext = value
		changed = true
	}
	if req.APIKey != nil && strings.TrimSpace(*req.APIKey) != "" {
		value, err := platformsecurity.EncryptSecret(strings.TrimSpace(*req.APIKey))
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
