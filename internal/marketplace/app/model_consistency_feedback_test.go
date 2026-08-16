package app

import (
	"testing"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestSubmitModelConsistencyFeedbackAggregatesAndBlocksOwner(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.ModelConsistencyFeedback{},
	))
	channel := marketplaceschema.Channel{
		ID: "feedback-channel", OwnerUserID: 7101, ProviderType: "openai_compatible",
		DeclaredModels:    `["gpt-5","claude-sonnet"]`,
		BaseURLCiphertext: "url", CredentialCiphertext: "key",
		Status: marketplacedomain.LifecycleActive,
	}
	group := marketplaceschema.Group{
		ID: "feedback-group", ChannelID: channel.ID, OwnerUserID: channel.OwnerUserID,
		PublicSlug: "feedback-public", SystemDisplayName: "反馈分组",
		InternalGroupName: "market_feedback", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1,
		LifecycleStatus:    marketplacedomain.LifecycleActive,
		VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility:         marketplacedomain.VisibilityPublic,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)

	_, err := SubmitModelConsistencyFeedback(channel.OwnerUserID, group.ID, ModelConsistencyFeedbackRequest{
		Model: "gpt-5", Status: marketplacedomain.ModelConsistencyPassed,
	})
	require.ErrorContains(t, err, "渠道主不能评价")

	first, err := SubmitModelConsistencyFeedback(7102, group.ID, ModelConsistencyFeedbackRequest{
		Model: "GPT-5", Status: marketplacedomain.ModelConsistencyPassed,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), first.Passed)
	require.Equal(t, marketplacedomain.ModelConsistencyPassed, first.ViewerStatus)

	updated, err := SubmitModelConsistencyFeedback(7102, group.ID, ModelConsistencyFeedbackRequest{
		Model: "gpt-5", Status: marketplacedomain.ModelConsistencyQuestioned,
	})
	require.NoError(t, err)
	require.Zero(t, updated.Passed)
	require.Equal(t, int64(1), updated.Questionable)
	require.Equal(t, int64(1), updated.Total)

	_, err = SubmitModelConsistencyFeedback(7103, group.ID, ModelConsistencyFeedbackRequest{
		Model: "missing-model", Status: marketplacedomain.ModelConsistencyFailed,
	})
	require.ErrorContains(t, err, "不在当前分组")

	var count int64
	require.NoError(t, db.Model(&marketplaceschema.ModelConsistencyFeedback{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}
