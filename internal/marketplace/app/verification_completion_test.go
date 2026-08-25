package app

import (
	"testing"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestCompleteVerificationKeepsFailedModelsUntilExplicitRemoval(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&gatewayschema.Channel{},
		&gatewayschema.Ability{},
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.VerificationRun{},
	))

	internal := gatewayschema.Channel{
		Key: "encrypted-key", Name: "marketplace-test", Status: 1,
		Models: "good-model,bad-model", Group: "market_test",
	}
	require.NoError(t, db.Create(&internal).Error)

	channel := marketplaceschema.Channel{
		ID: "channel-partial-pass", OwnerUserID: 1, ProviderType: "openai_compatible",
		BaseURLCiphertext: "encrypted-url", CredentialCiphertext: "encrypted-key",
		DeclaredModels: `["good-model","bad-model"]`, Status: marketplacedomain.LifecycleVerifying,
		InternalChannelID: &internal.Id,
	}
	group := marketplaceschema.Group{
		ID: "group-partial-pass", ChannelID: channel.ID, OwnerUserID: channel.OwnerUserID,
		PublicSlug: "partial-pass", SystemDisplayName: "部分通过渠道", InternalGroupName: "market_test",
		SourceType: marketplacedomain.SourceTypeMarketplaceUser, CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly,
		Multiplier: 1, LifecycleStatus: marketplacedomain.LifecycleVerifying,
		VerificationStatus: marketplacedomain.VerificationRunning, Visibility: marketplacedomain.VisibilityPublic,
	}
	run := marketplaceschema.VerificationRun{
		ID: "run-partial-pass", ChannelID: channel.ID, Status: marketplacedomain.VerificationRunning,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&run).Error)

	results := []ModelVerificationResult{
		{Model: "good-model", Status: marketplacedomain.ModelVerificationPassed, Listed: true},
		{Model: "bad-model", Status: marketplacedomain.ModelVerificationFailed, Listed: true},
	}
	completeVerification(&run, &channel, &group, results, nil)

	require.NoError(t, db.First(&channel, "id = ?", channel.ID).Error)
	require.Equal(t, []string{"good-model", "bad-model"}, decodeModels(channel.DeclaredModels))
	require.Equal(t, marketplacedomain.LifecycleDraft, channel.Status)

	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, marketplacedomain.LifecycleDraft, group.LifecycleStatus)
	require.Equal(t, marketplacedomain.VerificationFailed, group.VerificationStatus)

	require.NoError(t, db.First(&run, "id = ?", run.ID).Error)
	require.Equal(t, marketplacedomain.VerificationFailed, run.Status)
	require.Contains(t, run.Summary, "1 个声明模型未通过连通性检测")

	require.NoError(t, db.First(&internal, internal.Id).Error)
	require.Equal(t, "good-model,bad-model", internal.Models)
}
