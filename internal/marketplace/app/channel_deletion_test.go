package app

import (
	"testing"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOwnerCanOnlyDeleteOwnMarketplaceChannel(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&gatewayschema.Channel{},
		&gatewayschema.Ability{},
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.VerificationRun{},
		&marketplaceschema.RankingSnapshot{},
		&marketplaceschema.Settlement{},
		&marketplaceschema.AutoRoutePoolMember{},
	))

	internal := gatewayschema.Channel{Key: "encrypted", Status: 1, Models: "gpt-5", Group: "market-owned"}
	require.NoError(t, db.Create(&internal).Error)
	require.NoError(t, db.Create(&gatewayschema.Ability{
		Group: "market-owned", Model: "gpt-5", ChannelId: internal.Id, Enabled: true,
	}).Error)
	channel := marketplaceschema.Channel{
		ID: "123456789012", OwnerUserID: 42, ProviderType: "openai_compatible",
		BaseURLCiphertext: "encrypted-url", CredentialCiphertext: "encrypted-key",
		DeclaredModels: `["gpt-5"]`, Status: marketplacedomain.LifecycleActive,
		InternalChannelID: &internal.Id,
	}
	group := marketplaceschema.Group{
		ID: "group-delete", ChannelID: channel.ID, OwnerUserID: channel.OwnerUserID,
		PublicSlug: "delete-test", SystemDisplayName: "删除测试渠道", InternalGroupName: "market-owned",
		SourceType: marketplacedomain.SourceTypeMarketplaceUser, CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly,
		Multiplier: 1, LifecycleStatus: marketplacedomain.LifecycleActive,
		VerificationStatus: marketplacedomain.VerificationPassed, Visibility: marketplacedomain.VisibilityPublic,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&marketplaceschema.AutoRoutePoolMember{OwnerUserID: 7, GroupID: group.ID, Priority: 1}).Error)
	require.NoError(t, db.Create(&marketplaceschema.RankingSnapshot{GroupID: group.ID, WindowHours: 24, RankingVersion: rankingVersion}).Error)
	require.NoError(t, db.Create(&marketplaceschema.VerificationRun{ID: "verify-delete", ChannelID: channel.ID, Status: marketplacedomain.VerificationPassed}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{ID: "settlement-delete", RequestID: "request-delete", GroupID: group.ID, OwnerUserID: 42, ConsumerUserID: 7, Status: "released"}).Error)

	require.ErrorIs(t, DeleteOwnerChannel(7, channel.ID), gorm.ErrRecordNotFound)
	require.NoError(t, db.First(&marketplaceschema.Channel{}, "id = ?", channel.ID).Error)

	require.NoError(t, DeleteOwnerChannel(42, channel.ID))
	require.ErrorIs(t, db.First(&marketplaceschema.Channel{}, "id = ?", channel.ID).Error, gorm.ErrRecordNotFound)
	require.ErrorIs(t, db.First(&marketplaceschema.Group{}, "id = ?", group.ID).Error, gorm.ErrRecordNotFound)
	require.NoError(t, db.Unscoped().First(&marketplaceschema.Channel{}, "id = ?", channel.ID).Error)
	require.NoError(t, db.Unscoped().First(&marketplaceschema.Group{}, "id = ?", group.ID).Error)
	require.ErrorIs(t, db.First(&gatewayschema.Channel{}, internal.Id).Error, gorm.ErrRecordNotFound)
	require.ErrorIs(t, db.First(&gatewayschema.Ability{}, "channel_id = ?", internal.Id).Error, gorm.ErrRecordNotFound)
	require.ErrorIs(t, db.First(&marketplaceschema.AutoRoutePoolMember{}, "group_id = ?", group.ID).Error, gorm.ErrRecordNotFound)
	require.ErrorIs(t, db.First(&marketplaceschema.RankingSnapshot{}, "group_id = ?", group.ID).Error, gorm.ErrRecordNotFound)
	require.NoError(t, db.First(&marketplaceschema.VerificationRun{}, "channel_id = ?", channel.ID).Error)
	require.NoError(t, db.First(&marketplaceschema.Settlement{}, "group_id = ?", group.ID).Error)
}

func TestAdminCanDeleteAnotherOwnersMarketplaceChannel(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{}, &marketplaceschema.Group{},
		&marketplaceschema.RankingSnapshot{}, &marketplaceschema.AutoRoutePoolMember{},
	))
	channel := marketplaceschema.Channel{
		ID: "223456789012", OwnerUserID: 99, ProviderType: "anthropic",
		BaseURLCiphertext: "encrypted-url", CredentialCiphertext: "encrypted-key",
		Status: marketplacedomain.LifecycleDraft,
	}
	group := autoRouteTestGroup("admin-delete", channel.ID, channel.OwnerUserID, 1)
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)

	require.NoError(t, DeleteAdminChannel(channel.ID))
	require.ErrorIs(t, db.First(&marketplaceschema.Channel{}, "id = ?", channel.ID).Error, gorm.ErrRecordNotFound)
}

func TestMarketplaceChannelIDIncrements(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.ChannelIDSequence{}))
	id, err := newMarketplaceChannelID(db)
	require.NoError(t, err)
	require.Equal(t, "1", id)
	nextID, err := newMarketplaceChannelID(db)
	require.NoError(t, err)
	require.Equal(t, "2", nextID)
}
