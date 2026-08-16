package app

import (
	"testing"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestInternalGroupNameUsesReadableChannelIdentity(t *testing.T) {
	name := marketplaceInternalGroupName("Codex Plus", "ae381d8d10b94849")
	require.Equal(t, "Codex-Plus-ae381d", name)
}

func TestAdminCanUpdateMarketplaceChannelContent(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}))

	channel := marketplaceschema.Channel{
		ID: "channel-admin-edit", OwnerUserID: 42, ProviderType: "anthropic",
		SubmittedSourceLabel: "Claude Max", SourceLabelStatus: marketplacedomain.SourceLabelApproved,
		BaseURLCiphertext: "encrypted-url", CredentialCiphertext: "encrypted-key",
		DeclaredModels: `["claude-sonnet-4"]`, MaxConcurrency: 10, QPS: 5,
		Status: marketplacedomain.LifecycleActive,
	}
	group := marketplaceschema.Group{
		ID: "ae381d8d10b94849", ChannelID: channel.ID, OwnerUserID: channel.OwnerUserID,
		PublicSlug: "mg_admin_edit", SystemDisplayName: "用户分组 1.00x · #AE38",
		InternalGroupName: "market_u0100_ae381d8d10b9_v1", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1, RoutingVersion: 1,
		LifecycleStatus: marketplacedomain.LifecycleActive, VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility: marketplacedomain.VisibilityPublic,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)

	newSource := "CC-Kiro"
	newMultiplier := 0.5
	consistency := marketplacedomain.ModelConsistencyQuestioned
	updated, err := UpdateAdminChannel(channel.ID, AdminUpdateChannelRequest{
		UpdateChannelRequest: UpdateChannelRequest{
			SourceLabel: &newSource, Multiplier: &newMultiplier,
		},
		ModelConsistencyStatus: &consistency,
	})
	require.NoError(t, err)
	require.Equal(t, newSource, updated.SubmittedSourceLabel)
	require.Equal(t, marketplacedomain.SourceLabelApproved, updated.SourceLabelStatus)
	require.Equal(t, newMultiplier, updated.Multiplier)
	require.Equal(t, consistency, updated.ModelConsistencyStatus)
	var savedChannel marketplaceschema.Channel
	require.NoError(t, db.First(&savedChannel, "id = ?", channel.ID).Error)
	require.Equal(t, consistency, savedChannel.ModelConsistencyStatus)
	var savedGroup marketplaceschema.Group
	require.NoError(t, db.First(&savedGroup, "channel_id = ?", channel.ID).Error)
	require.Equal(t, "CC-Kiro-ae381d", savedGroup.InternalGroupName)
}

func TestModelConsistencyStatusValidation(t *testing.T) {
	channel := &marketplaceschema.Channel{}
	require.NoError(t, applyModelConsistencyStatus(channel, marketplacedomain.ModelConsistencyPassed))
	require.Equal(t, marketplacedomain.ModelConsistencyPassed, channel.ModelConsistencyStatus)
	require.EqualError(t, applyModelConsistencyStatus(channel, "owner-defined"), "模型一致性标注无效")
}

func TestOwnerCanDisableSensitiveWordInterception(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}))

	enabled := true
	channel := &marketplaceschema.Channel{ID: "sensitive-toggle", ProviderType: "openai_compatible", SensitiveWordInterceptionEnabled: &enabled}
	group := &marketplaceschema.Group{ID: "sensitive-toggle-group", InternalGroupName: "market_sensitive", Multiplier: 1}
	disabled := false

	_, err := applyChannelUpdate(channel, group, UpdateChannelRequest{
		SensitiveWordInterceptionEnabled: &disabled,
	})
	require.NoError(t, err)
	require.NotNil(t, channel.SensitiveWordInterceptionEnabled)
	require.False(t, *channel.SensitiveWordInterceptionEnabled)
	require.NoError(t, db.Create(channel).Error)

	var saved marketplaceschema.Channel
	require.NoError(t, db.First(&saved, "id = ?", channel.ID).Error)
	require.NotNil(t, saved.SensitiveWordInterceptionEnabled)
	require.False(t, *saved.SensitiveWordInterceptionEnabled)
}

func TestEditingChannelDoesNotResetVerificationState(t *testing.T) {
	channel := &marketplaceschema.Channel{
		ID: "edit-without-verification", ProviderType: "openai_compatible",
		DeclaredModels: `["gpt-4.1"]`, Status: marketplacedomain.LifecycleActive,
	}
	group := &marketplaceschema.Group{
		ID: "edit-without-verification-group", Multiplier: 1,
		LifecycleStatus:    marketplacedomain.LifecycleActive,
		VerificationStatus: marketplacedomain.VerificationPassed,
	}
	models := []string{"gpt-4.1", "gpt-4.1-mini"}

	requiresManualVerification, err := applyChannelUpdate(channel, group, UpdateChannelRequest{DeclaredModels: &models})

	require.NoError(t, err)
	require.True(t, requiresManualVerification)
	require.Equal(t, marketplacedomain.LifecycleActive, channel.Status)
	require.Equal(t, marketplacedomain.LifecycleActive, group.LifecycleStatus)
	require.Equal(t, marketplacedomain.VerificationPassed, group.VerificationStatus)
}
