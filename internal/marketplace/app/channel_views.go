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
	latest, _ := LatestVerification(channel.ID)
	return channelViewWithLatestVerification(channel, group, latest)
}

func channelViewWithLatestVerification(
	channel *marketplaceschema.Channel,
	group *marketplaceschema.Group,
	latest *marketplaceschema.VerificationRun,
) *ChannelView {
	view := &ChannelView{
		ID: channel.ID, OwnerUserID: channel.OwnerUserID, GroupID: group.ID, PublicSlug: group.PublicSlug,
		SystemDisplayName:    marketplaceDisplayName(channel.SubmittedSourceLabel, group.Multiplier, channel.ID),
		ProviderType:         channel.ProviderType,
		SubmittedSourceLabel: channel.SubmittedSourceLabel, ApprovedSourceLabel: channel.ApprovedSourceLabel,
		SourceLabelStatus: channel.SourceLabelStatus, SourceLabelReviewReason: channel.SourceLabelReviewReason,
		CredentialTail: channel.CredentialTail, CredentialVersion: channel.CredentialVersion,
		DeclaredModels:            decodeModels(channel.DeclaredModels),
		ModelPrices:               decodeChannelModelPrices(channel.ModelPrices),
		ModelVerificationResults:  decodeModelVerificationResults(channel.ModelVerificationResults),
		ConnectivityTestStatus:    channel.ConnectivityTestStatus,
		ConnectivityTestCheckedAt: channel.ConnectivityTestCheckedAt,
		ModelConsistencyStatus:    channel.ModelConsistencyStatus,
		AutoProbeEnabled:          channel.AutoProbeEnabled,
		AutoProbeIntervalMinutes:  channel.AutoProbeIntervalMinutes,
		AutoProbeModel:            channel.AutoProbeModel,
		AutoProbeLastStatus:       channel.AutoProbeLastStatus,
		AutoProbeLastAt:           channel.AutoProbeLastAt,
		Multiplier:                group.Multiplier, LifecycleStatus: group.LifecycleStatus,
		VerificationStatus: group.VerificationStatus, Visibility: group.Visibility,
		MaxConcurrency: channel.MaxConcurrency, UserMaxConcurrency: channel.UserMaxConcurrency, QPS: channel.QPS,
		MaintenanceWindow: channel.MaintenanceWindow, InternalChannelID: channel.InternalChannelID,
		SensitiveWordInterceptionEnabled: marketplaceSensitiveWordInterceptionEnabled(channel),
		MultiplierCardSupported:          channel.MultiplierCardSupported,
		MultiplierCardUserEnabled:        channel.MultiplierCardUserEnabled,
		LastReviewReason:                 channel.LastReviewReason, VerificationDueAt: group.VerificationDueAt,
		CreatedAt: channel.CreatedAt, UpdatedAt: channel.UpdatedAt,
	}
	if channel.DeletedAt.Valid {
		deletedAt := channel.DeletedAt.Time
		view.DeletedAt = &deletedAt
	}
	if latest != nil {
		view.VerificationStage = latest.Stage
		view.VerificationSummary = latest.Summary
		view.VerificationDetectorVersion = latest.DetectorVersion
		view.VerificationStartedAt = latest.StartedAt
		view.VerificationCompletedAt = latest.CompletedAt
	}
	return view
}

func latestVerifications(channelIDs []string) (map[string]*marketplaceschema.VerificationRun, error) {
	result := make(map[string]*marketplaceschema.VerificationRun, len(channelIDs))
	if len(channelIDs) == 0 {
		return result, nil
	}
	table := marketplaceschema.VerificationRun{}.TableName()
	query := "SELECT * FROM (SELECT vr.*, ROW_NUMBER() OVER (PARTITION BY channel_id ORDER BY created_at DESC, id DESC) AS row_num FROM " + table + " vr WHERE channel_id IN ?) ranked WHERE row_num = 1"
	var runs []marketplaceschema.VerificationRun
	if err := platformdb.DB.Raw(query, channelIDs).Scan(&runs).Error; err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "no such table") || strings.Contains(message, "does not exist") {
			return result, nil
		}
		return nil, err
	}
	for index := range runs {
		result[runs[index].ChannelID] = &runs[index]
	}
	return result, nil
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
