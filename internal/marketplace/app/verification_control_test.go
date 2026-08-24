package app

import (
	"context"
	"testing"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestVerificationTaskRegistryCancelsChannelTasks(t *testing.T) {
	registry := verificationTaskRegistry{
		tasks: make(map[verificationTaskKey]verificationTask),
	}
	ctx, finish, started := registry.begin(
		context.Background(), "channel-cancel", verificationTaskGPT56Mapping,
	)
	require.True(t, started)
	require.True(t, registry.active("channel-cancel", verificationTaskGPT56Mapping))

	require.True(t, registry.cancelChannel("channel-cancel"))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.False(t, registry.active("channel-cancel", verificationTaskGPT56Mapping))
	_, secondFinish, started := registry.begin(
		context.Background(), "channel-cancel", verificationTaskGPT56Mapping,
	)
	require.True(t, started)
	finish()
	require.True(t, registry.active("channel-cancel", verificationTaskGPT56Mapping))
	secondFinish()
}

func TestPauseChannelVerificationPreventsLateCompletion(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.VerificationRun{},
		&marketplaceschema.GPT56MappingRun{},
	))
	channel, group := createRunningVerificationFixture(t)
	nativeRun := marketplaceschema.VerificationRun{
		ID: "native-running", ChannelID: channel.ID,
		Status: marketplacedomain.VerificationRunning,
	}
	mappingRun := marketplaceschema.GPT56MappingRun{
		ID: "mapping-running", ChannelID: channel.ID,
		Status: GPT56MappingStatusRunning, Level: GPT56MappingLevelConfirmation,
		Trigger: GPT56MappingTriggerManual, Results: `[{"requested_model":"gpt-5.6-sol"}]`,
		StartedAt: time.Now().UTC(),
	}
	require.NoError(t, db.Create(&nativeRun).Error)
	require.NoError(t, db.Create(&mappingRun).Error)

	require.NoError(t, PauseChannelVerification(channel.ID))
	require.NoError(t, db.First(&channel, "id = ?", channel.ID).Error)
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.NoError(t, db.First(&nativeRun, "id = ?", nativeRun.ID).Error)
	require.NoError(t, db.First(&mappingRun, "id = ?", mappingRun.ID).Error)
	require.Equal(t, marketplacedomain.VerificationPaused, channel.ConnectivityTestStatus)
	require.Equal(t, GPT56MappingStatusPaused, channel.GPT56MappingStatus)
	require.Equal(t, marketplacedomain.LifecycleDraft, channel.Status)
	require.Equal(t, marketplacedomain.VerificationPaused, group.VerificationStatus)
	require.Equal(t, marketplacedomain.VerificationPaused, nativeRun.Status)
	require.Equal(t, GPT56MappingStatusPaused, mappingRun.Status)
	require.NotNil(t, nativeRun.CompletedAt)
	require.NotNil(t, mappingRun.CompletedAt)

	err := finishGPT56MappingRun(
		&mappingRun,
		[]GPT56MappingResult{{RequestedModel: "gpt-5.6-sol", Status: GPT56MappingStatusMatched}},
		GPT56MappingStatusMatched,
	)
	require.ErrorIs(t, err, errVerificationNotRunning)
	completeVerification(&nativeRun, &channel, &group, nil, nil)

	require.NoError(t, db.First(&channel, "id = ?", channel.ID).Error)
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, GPT56MappingStatusPaused, channel.GPT56MappingStatus)
	require.Equal(t, marketplacedomain.VerificationPaused, group.VerificationStatus)
}

func TestPrepareGPT56VerificationRecoversOrphanedRun(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.GPT56MappingRun{},
	))
	channel, group := createRunningVerificationFixture(t)
	run := marketplaceschema.GPT56MappingRun{
		ID: "orphaned-mapping", ChannelID: channel.ID,
		Status: GPT56MappingStatusRunning, Level: GPT56MappingLevelConfirmation,
		Trigger: GPT56MappingTriggerManual, Results: "[]", StartedAt: time.Now().UTC(),
	}
	require.NoError(t, db.Create(&run).Error)

	require.NoError(t, prepareGPT56MappingVerification(channel.ID))
	require.NoError(t, db.First(&channel, "id = ?", channel.ID).Error)
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.NoError(t, db.First(&run, "id = ?", run.ID).Error)
	require.Equal(t, GPT56MappingStatusQueued, channel.GPT56MappingStatus)
	require.Equal(t, marketplacedomain.VerificationQueued, group.VerificationStatus)
	require.Equal(t, GPT56MappingStatusPaused, run.Status)
	require.NotNil(t, run.CompletedAt)
}

func TestEditingChannelCancelsAndReleasesRunningVerification(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{}, &marketplaceschema.Group{},
		&marketplaceschema.VerificationRun{}, &marketplaceschema.GPT56MappingRun{},
	))
	channel, _ := createRunningVerificationFixture(t)
	nativeRun := marketplaceschema.VerificationRun{
		ID: "edit-native-running", ChannelID: channel.ID,
		Status: marketplacedomain.VerificationRunning,
	}
	mappingRun := marketplaceschema.GPT56MappingRun{
		ID: "edit-mapping-running", ChannelID: channel.ID,
		Status: GPT56MappingStatusRunning, Level: GPT56MappingLevelConfirmation,
		Trigger: GPT56MappingTriggerManual, Results: "[]", StartedAt: time.Now().UTC(),
	}
	require.NoError(t, db.Create(&nativeRun).Error)
	require.NoError(t, db.Create(&mappingRun).Error)
	ctx, finish, started := marketplaceVerificationTasks.begin(
		context.Background(), channel.ID, verificationTaskGPT56Mapping,
	)
	require.True(t, started)
	t.Cleanup(finish)

	originalQueue := queueMarketplaceCapabilityProbe
	queueMarketplaceCapabilityProbe = func(string) {}
	t.Cleanup(func() { queueMarketplaceCapabilityProbe = originalQueue })
	models := []string{"gpt-5.6-sol", "gpt-5.6-pro"}
	_, err := UpdateOwnerChannel(channel.OwnerUserID, channel.ID, UpdateChannelRequest{DeclaredModels: &models})
	require.NoError(t, err)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.False(t, marketplaceVerificationTasks.active(channel.ID, verificationTaskGPT56Mapping))

	_, secondFinish, started := marketplaceVerificationTasks.begin(
		context.Background(), channel.ID, verificationTaskGPT56Mapping,
	)
	require.True(t, started)
	secondFinish()
	require.NoError(t, db.First(&nativeRun, "id = ?", nativeRun.ID).Error)
	require.NoError(t, db.First(&mappingRun, "id = ?", mappingRun.ID).Error)
	require.Equal(t, marketplacedomain.VerificationPaused, nativeRun.Status)
	require.Equal(t, GPT56MappingStatusPaused, mappingRun.Status)
}

func createRunningVerificationFixture(
	t *testing.T,
) (marketplaceschema.Channel, marketplaceschema.Group) {
	t.Helper()
	channel := marketplaceschema.Channel{
		ID: "running-channel", OwnerUserID: 7, ProviderType: "codex",
		BaseURLCiphertext: "encrypted-url", CredentialCiphertext: "encrypted-key",
		DeclaredModels: `["gpt-5.6-sol"]`, Status: marketplacedomain.LifecycleVerifying,
		ConnectivityTestStatus: marketplacedomain.VerificationRunning,
		GPT56MappingStatus:     GPT56MappingStatusRunning,
		GPT56MappingResults:    `[{"requested_model":"gpt-5.6-sol","status":"running"}]`,
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
