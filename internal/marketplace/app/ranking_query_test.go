package app

import (
	"encoding/json"
	"testing"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestPublicSourceLabelRequiresApproval(t *testing.T) {
	channel := marketplaceschema.Channel{
		SubmittedSourceLabel: "Claude Max",
		ApprovedSourceLabel:  "Claude Max",
		SourceLabelStatus:    marketplacedomain.SourceLabelPending,
	}
	require.Empty(t, publicSourceLabel(channel))

	channel.SourceLabelStatus = marketplacedomain.SourceLabelApproved
	require.Equal(t, "Claude Max", publicSourceLabel(channel))
}

func TestPendingSourceLabelIsNotSearchableOrReturned(t *testing.T) {
	group := marketplaceschema.Group{
		ID: "group-1", ChannelID: "channel-1", SystemDisplayName: "用户分组",
		OwnerDisplayName: "渠道主", PublicSlug: "mg_group1",
		LifecycleStatus: marketplacedomain.LifecyclePendingReview,
		Visibility:      marketplacedomain.VisibilityPublic,
	}
	channel := marketplaceschema.Channel{
		ID: "channel-1", SubmittedSourceLabel: "Kiro", ApprovedSourceLabel: "Kiro",
		SourceLabelStatus: marketplacedomain.SourceLabelPending,
	}
	require.False(t, matchesGroupQuery(group, channel, nil, GroupQuery{Search: "Kiro"}))
	require.False(t, matchesGroupQuery(group, channel, nil, GroupQuery{Search: "渠道主"}))
	item := groupListItem(group, channel, nil, marketplaceschema.RankingSnapshot{})
	require.Empty(t, item.SourceLabel)
	require.Equal(t, "来源待审核-0x-channel-1", item.SystemDisplayName)
}

func TestMarketplaceGroupFiltersByNumericChannelIDModelSourceAndProvider(t *testing.T) {
	group := marketplaceschema.Group{ID: "group-filter", ChannelID: "123456789012", SystemDisplayName: "Codex Plus"}
	channel := marketplaceschema.Channel{
		ID: "123456789012", ProviderType: "openai_compatible",
		ApprovedSourceLabel: "Codex Plus", SourceLabelStatus: marketplacedomain.SourceLabelApproved,
	}
	models := []string{"gpt-5.2-codex", "gpt-4.1"}

	require.True(t, matchesGroupQuery(group, channel, models, GroupQuery{Search: "123456"}))
	require.True(t, matchesGroupQuery(group, channel, models, GroupQuery{Model: "5.2"}))
	require.True(t, matchesGroupQuery(group, channel, models, GroupQuery{Source: "Codex Plus"}))
	require.True(t, matchesGroupQuery(group, channel, models, GroupQuery{Provider: "openai_compatible"}))
	require.False(t, matchesGroupQuery(group, channel, models, GroupQuery{Source: "CC-Kiro"}))
	require.False(t, matchesGroupQuery(group, channel, models, GroupQuery{Provider: "anthropic"}))

	item := groupListItem(group, channel, models, marketplaceschema.RankingSnapshot{})
	require.Equal(t, channel.ID, item.ChannelID)
	require.Equal(t, channel.ProviderType, item.ProviderType)
	require.Equal(t, "Codex Plus-0x-123456789012", item.SystemDisplayName)

	payload, err := json.Marshal(item)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "owner_display_name")
}

func TestPublicPendingReviewGroupIsLoadedForMarketplace(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}))

	channel := marketplaceschema.Channel{
		ID: "channel-pending", OwnerUserID: 10, ProviderType: "anthropic",
		SubmittedSourceLabel: "Claude Max", SourceLabelStatus: marketplacedomain.SourceLabelPending,
		BaseURLCiphertext: "encrypted-url", CredentialCiphertext: "encrypted-key",
		Status: marketplacedomain.LifecyclePendingReview,
	}
	group := marketplaceschema.Group{
		ID: "group-pending", ChannelID: channel.ID, OwnerUserID: 10,
		PublicSlug: "mg_pending", SystemDisplayName: "待审核公开分组",
		InternalGroupName: "market_pending", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1,
		LifecycleStatus:    marketplacedomain.LifecyclePendingReview,
		VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility:         marketplacedomain.VisibilityPublic,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)

	groups, channels, err := loadPublicGroups(GroupQuery{})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, group.ID, groups[0].ID)
	require.Empty(t, publicSourceLabel(channels[channel.ID]))
}
