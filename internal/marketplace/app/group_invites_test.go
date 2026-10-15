package app

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceGroupInviteLifecycle(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.GroupInvite{},
		&marketplaceschema.GroupAccess{},
	))

	group := marketplaceschema.Group{
		ID: "invite-group", ChannelID: "invite-channel", OwnerUserID: 10,
		PublicSlug: "invite-market-group", SystemDisplayName: "邀请制分组",
		InternalGroupName: "market_u10_invite_1x_group_v1",
		SourceType:        marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy:  marketplacedomain.CreditPolicyUniversalOnly,
		Multiplier:        1, LifecycleStatus: marketplacedomain.LifecycleActive,
		VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility:         marketplacedomain.VisibilityPrivate,
	}
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&marketplaceschema.Channel{
		ID: group.ChannelID, OwnerUserID: group.OwnerUserID,
		ProviderType: "openai_compatible", BaseURLCiphertext: "url",
		CredentialCiphertext: "key",
	}).Error)

	_, err := CreateMarketplaceGroupInvite(20, group.ID)
	require.EqualError(t, err, "无权为该分组创建邀请链接")

	first, err := CreateMarketplaceGroupInvite(group.OwnerUserID, group.ID)
	require.NoError(t, err)
	require.NotEmpty(t, first.Token)

	second, err := CreateMarketplaceGroupInvite(group.OwnerUserID, group.ID)
	require.NoError(t, err)
	require.NotEqual(t, first.Token, second.Token)

	_, err = AcceptMarketplaceGroupInvite(20, first.Token)
	require.EqualError(t, err, "邀请链接无效或已失效")

	accepted, err := AcceptMarketplaceGroupInvite(20, second.Token)
	require.NoError(t, err)
	require.Equal(t, group.ID, accepted.GroupID)

	_, err = AcceptMarketplaceGroupInvite(20, second.Token)
	require.NoError(t, err)
	var accessCount int64
	require.NoError(t, db.Model(&marketplaceschema.GroupAccess{}).
		Where("group_id = ? AND user_id = ?", group.ID, 20).
		Count(&accessCount).Error)
	require.Equal(t, int64(1), accessCount)

	binding, err := ResolveTokenGroupBinding(TokenGroupValue(group.ID), 20)
	require.NoError(t, err)
	require.Equal(t, group.InternalGroupName, binding.InternalGroup)
}

func TestMarketplaceGroupInviteRejectsInvalidAndExpiredTokens(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.GroupInvite{},
		&marketplaceschema.GroupAccess{},
	))

	_, err := AcceptMarketplaceGroupInvite(20, "not-an-invite")
	require.EqualError(t, err, "邀请链接无效或已失效")

	rawToken := "expired-invite"
	hash := sha256.Sum256([]byte(rawToken))
	expiredAt := time.Now().UTC().Add(-time.Hour)
	require.NoError(t, db.Create(&marketplaceschema.GroupInvite{
		GroupID: "expired-group", CreatedBy: 10,
		TokenHash: base64.RawURLEncoding.EncodeToString(hash[:]),
		ExpiresAt: &expiredAt,
	}).Error)

	_, err = AcceptMarketplaceGroupInvite(20, rawToken)
	require.EqualError(t, err, "邀请链接已过期")
}
