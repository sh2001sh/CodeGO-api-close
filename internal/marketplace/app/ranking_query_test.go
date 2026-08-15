package app

import (
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
	require.Empty(t, groupListItem(group, channel, nil, marketplaceschema.RankingSnapshot{}).SourceLabel)
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
