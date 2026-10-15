package app

import (
	"context"
	"testing"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestVerificationTaskRegistryCancelsChannelTasks(t *testing.T) {
	registry := verificationTaskRegistry{tasks: make(map[verificationTaskKey]verificationTask)}
	ctx, finish, started := registry.begin(context.Background(), "channel-cancel", verificationTaskConnectivity)
	require.True(t, started)
	require.True(t, registry.active("channel-cancel", verificationTaskConnectivity))

	require.True(t, registry.cancelChannel("channel-cancel"))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.False(t, registry.active("channel-cancel", verificationTaskConnectivity))
	_, secondFinish, started := registry.begin(context.Background(), "channel-cancel", verificationTaskConnectivity)
	require.True(t, started)
	finish()
	require.True(t, registry.active("channel-cancel", verificationTaskConnectivity))
	secondFinish()
}

func TestPauseChannelVerificationPreventsLateCompletion(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{}, &marketplaceschema.Group{}, &marketplaceschema.VerificationRun{},
	))
	channel, group := createRunningVerificationFixture(t)
	run := marketplaceschema.VerificationRun{
		ID: "native-running", ChannelID: channel.ID, Status: marketplacedomain.VerificationRunning,
	}
	require.NoError(t, db.Create(&run).Error)

	require.NoError(t, PauseChannelVerification(channel.ID))
	require.NoError(t, db.First(&channel, "id = ?", channel.ID).Error)
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.NoError(t, db.First(&run, "id = ?", run.ID).Error)
	require.Equal(t, marketplacedomain.VerificationPaused, channel.ConnectivityTestStatus)
	require.Equal(t, marketplacedomain.LifecycleDraft, channel.Status)
	require.Equal(t, marketplacedomain.VerificationPaused, group.VerificationStatus)
	require.Equal(t, marketplacedomain.VerificationPaused, run.Status)
	require.NotNil(t, run.CompletedAt)

	completeVerification(&run, &channel, &group, nil, nil)
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, marketplacedomain.VerificationPaused, group.VerificationStatus)
}

func TestEditingChannelCancelsAndReleasesRunningVerification(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{}, &marketplaceschema.Group{}, &marketplaceschema.VerificationRun{},
	))
	channel, _ := createRunningVerificationFixture(t)
	run := marketplaceschema.VerificationRun{
		ID: "edit-native-running", ChannelID: channel.ID, Status: marketplacedomain.VerificationRunning,
	}
	require.NoError(t, db.Create(&run).Error)
	ctx, finish, started := marketplaceVerificationTasks.begin(
		context.Background(), channel.ID, verificationTaskConnectivity,
	)
	require.True(t, started)
	t.Cleanup(finish)

	originalQueue := queueMarketplaceCapabilityProbe
	queueMarketplaceCapabilityProbe = func(string) {}
	t.Cleanup(func() { queueMarketplaceCapabilityProbe = originalQueue })
	provider := "openai_compatible"
	_, err := UpdateOwnerChannel(channel.OwnerUserID, channel.ID, UpdateChannelRequest{ProviderType: &provider})
	require.NoError(t, err)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.False(t, marketplaceVerificationTasks.active(channel.ID, verificationTaskConnectivity))
	require.NoError(t, db.First(&run, "id = ?", run.ID).Error)
	require.Equal(t, marketplacedomain.VerificationPaused, run.Status)
}

func createRunningVerificationFixture(t *testing.T) (marketplaceschema.Channel, marketplaceschema.Group) {
	t.Helper()
	channel := marketplaceschema.Channel{
		ID: "running-channel", OwnerUserID: 7, ProviderType: "codex",
		BaseURLCiphertext: "encrypted-url", CredentialCiphertext: "encrypted-key",
		DeclaredModels: `["gpt-5.6-sol"]`, Status: marketplacedomain.LifecycleVerifying,
		ConnectivityTestStatus: marketplacedomain.VerificationRunning,
	}
	group := marketplaceschema.Group{
		ID: "running-group", ChannelID: channel.ID, OwnerUserID: channel.OwnerUserID,
		PublicSlug: "running-group", SystemDisplayName: "运行中渠道",
		InternalGroupName: "market_running", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1,
		LifecycleStatus:    marketplacedomain.LifecycleVerifying,
		VerificationStatus: marketplacedomain.VerificationRunning,
		Visibility:         marketplacedomain.VisibilityPublic,
	}
	require.NoError(t, platformdb.DB.Create(&channel).Error)
	require.NoError(t, platformdb.DB.Create(&group).Error)
	return channel, group
}
