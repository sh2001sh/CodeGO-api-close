package app

import (
	"errors"
	"strings"

	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

func channelView(channel *marketplaceschema.Channel, group *marketplaceschema.Group) *ChannelView {
	view := &ChannelView{
		ID: channel.ID, GroupID: group.ID, PublicSlug: group.PublicSlug,
		SystemDisplayName:    marketplaceDisplayName(channel.SubmittedSourceLabel, group.Multiplier, channel.ID),
		ProviderType:         channel.ProviderType,
		SubmittedSourceLabel: channel.SubmittedSourceLabel, ApprovedSourceLabel: channel.ApprovedSourceLabel,
		SourceLabelStatus: channel.SourceLabelStatus, SourceLabelReviewReason: channel.SourceLabelReviewReason,
		CredentialTail: channel.CredentialTail, CredentialVersion: channel.CredentialVersion,
		DeclaredModels:           decodeModels(channel.DeclaredModels),
		ModelPrices:              decodeChannelModelPrices(channel.ModelPrices),
		ModelVerificationResults: decodeModelVerificationResults(channel.ModelVerificationResults),
		ModelConsistencyStatus:   channel.ModelConsistencyStatus,
		GPT56MappingResults:      decodeGPT56MappingResults(channel.GPT56MappingResults),
		GPT56MappingStatus:       channel.GPT56MappingStatus,
		GPT56MappingCheckedAt:    channel.GPT56MappingCheckedAt,
		Multiplier:               group.Multiplier, LifecycleStatus: group.LifecycleStatus,
		VerificationStatus: group.VerificationStatus, Visibility: group.Visibility,
		MaxConcurrency: channel.MaxConcurrency, QPS: channel.QPS,
		MaintenanceWindow: channel.MaintenanceWindow, InternalChannelID: channel.InternalChannelID,
		SensitiveWordInterceptionEnabled: marketplaceSensitiveWordInterceptionEnabled(channel),
		LastReviewReason:                 channel.LastReviewReason, VerificationDueAt: group.VerificationDueAt,
		CreatedAt: channel.CreatedAt, UpdatedAt: channel.UpdatedAt,
	}
	if latest, err := LatestVerification(channel.ID); err == nil && latest != nil {
		view.VerificationStage = latest.Stage
		view.VerificationSummary = latest.Summary
		view.VerificationDetectorVersion = latest.DetectorVersion
		view.VerificationStartedAt = latest.StartedAt
		view.VerificationCompletedAt = latest.CompletedAt
	}
	return view
}

func loadOwnerDisplayName(userID int) string {
	var user identityschema.User
	if err := platformdb.DB.Select("display_name", "username").First(&user, userID).Error; err != nil {
		return "渠道主"
	}
	if strings.TrimSpace(user.DisplayName) != "" {
		return strings.TrimSpace(user.DisplayName)
	}
	return strings.TrimSpace(user.Username)
}

func marketplaceSensitiveWordInterceptionEnabled(channel *marketplaceschema.Channel) bool {
	return channel == nil || channel.SensitiveWordInterceptionEnabled == nil || *channel.SensitiveWordInterceptionEnabled
}

func isNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
