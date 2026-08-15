package app

import (
	"testing"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestAutoRoutePoolOrdersByMultiplierAndAvailability(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.RankingSnapshot{},
		&marketplaceschema.AutoRoutePoolMember{},
	))
	stableChannelID, cheapChannelID, otherChannelID := 101, 102, 103
	require.NoError(t, db.Create([]marketplaceschema.Channel{
		{ID: "stable-channel", OwnerUserID: 11, ProviderType: "openai", DeclaredModels: `["gpt-5"]`, InternalChannelID: &stableChannelID, Status: marketplacedomain.LifecycleActive},
		{ID: "cheap-channel", OwnerUserID: 12, ProviderType: "openai", DeclaredModels: `["gpt-5"]`, InternalChannelID: &cheapChannelID, Status: marketplacedomain.LifecycleActive},
		{ID: "other-channel", OwnerUserID: 13, ProviderType: "anthropic", DeclaredModels: `["claude-sonnet"]`, InternalChannelID: &otherChannelID, Status: marketplacedomain.LifecycleActive},
	}).Error)
	require.NoError(t, db.Create([]marketplaceschema.Group{
		autoRouteTestGroup("stable", "stable-channel", 11, 1),
		autoRouteTestGroup("cheap", "cheap-channel", 12, 0.5),
		autoRouteTestGroup("other", "other-channel", 13, 0.2),
	}).Error)
	require.NoError(t, db.Create([]marketplaceschema.RankingSnapshot{
		{GroupID: "stable", WindowHours: 24, RankingVersion: rankingVersion, WilsonSuccessRate: 95, RequestCount: 500},
		{GroupID: "cheap", WindowHours: 24, RankingVersion: rankingVersion, WilsonSuccessRate: 50, RequestCount: 500},
		{GroupID: "other", WindowHours: 24, RankingVersion: rankingVersion, WilsonSuccessRate: 99, RequestCount: 500},
	}).Error)

	view, err := ReplaceAutoRoutePool(20, AutoRoutePoolUpdateRequest{GroupIDs: []string{"cheap", "stable", "other"}})
	require.NoError(t, err)
	require.Equal(t, 3, view.SelectedCount)

	bindings, err := ResolveAutoRouteBindings(20, "gpt-5")
	require.NoError(t, err)
	require.Len(t, bindings, 2)
	require.Equal(t, "stable", bindings[0].GroupID)
	require.Equal(t, "cheap", bindings[1].GroupID)
}

func TestAutoRoutePoolRejectsForeignPrivateGroup(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.RankingSnapshot{},
		&marketplaceschema.AutoRoutePoolMember{},
	))
	internalChannelID := 101
	require.NoError(t, db.Create(&marketplaceschema.Channel{
		ID: "private-channel", OwnerUserID: 11, ProviderType: "openai",
		DeclaredModels: `["gpt-5"]`, InternalChannelID: &internalChannelID,
	}).Error)
	group := autoRouteTestGroup("private", "private-channel", 11, 1)
	group.Visibility = marketplacedomain.VisibilityPrivate
	require.NoError(t, db.Create(&group).Error)

	_, err := ReplaceAutoRoutePool(20, AutoRoutePoolUpdateRequest{GroupIDs: []string{"private"}})
	require.EqualError(t, err, "路由池包含不可用或无权访问的第三方分组")
}

func TestMarketplaceAutoTokenGroupIsReserved(t *testing.T) {
	require.True(t, IsMarketplaceAutoTokenGroup("market:auto"))
	_, err := ResolveTokenGroupBinding("market:auto", 20)
	require.EqualError(t, err, "第三方 Auto 分组需要在请求模型确定后解析")
}

func autoRouteTestGroup(id, channelID string, ownerID int, multiplier float64) marketplaceschema.Group {
	return marketplaceschema.Group{
		ID: id, ChannelID: channelID, OwnerUserID: ownerID,
		PublicSlug: id, SystemDisplayName: id, InternalGroupName: "market-" + id,
		SourceType:       marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly,
		Multiplier:       multiplier, LifecycleStatus: marketplacedomain.LifecycleActive,
		VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility:         marketplacedomain.VisibilityPublic,
	}
}
