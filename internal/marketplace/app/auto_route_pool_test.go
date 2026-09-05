package app

import (
	"fmt"
	"testing"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestAutoRoutePoolHonorsUserPriority(t *testing.T) {
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
		{ID: "cheap-channel", OwnerUserID: 12, ProviderType: "openai", DeclaredModels: `["gpt-5"]`, ModelPrices: `{"gpt-5":{"input_price_per_million":4,"output_price_per_million":12}}`, InternalChannelID: &cheapChannelID, Status: marketplacedomain.LifecycleActive},
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
	models, configured, err := ListSelectedAutoRouteModels(20)
	require.NoError(t, err)
	require.True(t, configured)
	require.Equal(t, []string{"claude-sonnet", "gpt-5"}, models)

	bindings, err := ResolveAutoRouteBindings(20, "gpt-5", 0)
	require.NoError(t, err)
	require.Len(t, bindings, 2)
	require.Equal(t, "cheap", bindings[0].GroupID)
	require.Equal(t, float64(4), bindings[0].ModelPrices["gpt-5"].InputPricePerMillion)
	require.Equal(t, "stable", bindings[1].GroupID)
	require.Equal(t, "cheap", view.Items[0].GroupID)
	require.Equal(t, 1, view.Items[0].Priority)
}

func TestNormalizeAutoRoutePoolConfigClampsPositiveMultiplierCeiling(t *testing.T) {
	config := normalizeAutoRoutePoolConfig(&AutoRoutePoolConfig{MaxMultiplier: 0.0001})
	if config.MaxMultiplier != 0.001 {
		t.Fatalf("max multiplier = %v, want 0.001", config.MaxMultiplier)
	}
	if unlimited := normalizeAutoRoutePoolConfig(&AutoRoutePoolConfig{MaxMultiplier: 0}).MaxMultiplier; unlimited != 0 {
		t.Fatalf("zero max multiplier = %v, want unlimited zero", unlimited)
	}
}

func TestLoadAutoRouteGroupsForIDsOnlyLoadsSelectedGroups(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}))
	selectedChannelID, unrelatedChannelID := 501, 502
	require.NoError(t, db.Create([]marketplaceschema.Channel{
		{ID: "selected-channel", OwnerUserID: 11, ProviderType: "openai", DeclaredModels: `["gpt-5"]`, InternalChannelID: &selectedChannelID, Status: marketplacedomain.LifecycleActive},
		{ID: "unrelated-channel", OwnerUserID: 12, ProviderType: "openai", DeclaredModels: `["gpt-5"]`, InternalChannelID: &unrelatedChannelID, Status: marketplacedomain.LifecycleActive},
	}).Error)
	require.NoError(t, db.Create([]marketplaceschema.Group{
		autoRouteTestGroup("selected-group", "selected-channel", 11, 1),
		autoRouteTestGroup("unrelated-group", "unrelated-channel", 12, 1),
	}).Error)

	groups, channels, err := loadAutoRouteGroupsForIDs(20, []string{"selected-group"})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, "selected-group", groups[0].ID)
	require.Contains(t, channels, "selected-channel")
	require.NotContains(t, channels, "unrelated-channel")

	groups, channels, err = loadAutoRouteGroupsForIDs(20, []string{})
	require.NoError(t, err)
	require.Empty(t, groups)
	require.Empty(t, channels)
}

func TestAutoRoutePoolRejectsMoreThanTenGroups(t *testing.T) {
	groupIDs := make([]string, 11)
	for i := range groupIDs {
		groupIDs[i] = fmt.Sprintf("group-%d", i+1)
	}

	_, err := ReplaceAutoRoutePool(20, AutoRoutePoolUpdateRequest{GroupIDs: groupIDs})
	require.EqualError(t, err, "全局 Auto 路由池最多可添加 10 个分组")
}

func TestCreateRoutePoolReturnsLightweightViewAndRejectsDuplicateName(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.RoutePool{}, &marketplaceschema.RoutePoolMember{}))

	view, err := CreateRoutePool(20, RoutePoolCreateRequest{Name: "自动池"})
	require.NoError(t, err)
	require.NotNil(t, view)
	require.Equal(t, "自动池", view.Name)
	require.Empty(t, view.Items)
	require.Equal(t, "priority", view.Config.Strategy)

	_, err = CreateRoutePool(20, RoutePoolCreateRequest{Name: "自动池"})
	require.EqualError(t, err, "路由池名称已存在，请使用其他名称")
	require.NoError(t, DeleteRoutePool(20, "missing-route-pool"))
}

func TestAutoRoutePoolFiltersGroupsAboveTokenMultiplierLimit(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.RankingSnapshot{},
		&marketplaceschema.AutoRoutePoolMember{},
	))
	cheapChannelID, expensiveChannelID := 201, 202
	require.NoError(t, db.Create([]marketplaceschema.Channel{
		{ID: "cheap-channel", OwnerUserID: 11, ProviderType: "openai", DeclaredModels: `["gpt-5"]`, InternalChannelID: &cheapChannelID, Status: marketplacedomain.LifecycleActive},
		{ID: "expensive-channel", OwnerUserID: 12, ProviderType: "openai", DeclaredModels: `["gpt-5"]`, InternalChannelID: &expensiveChannelID, Status: marketplacedomain.LifecycleActive},
	}).Error)
	require.NoError(t, db.Create([]marketplaceschema.Group{
		autoRouteTestGroup("cheap", "cheap-channel", 11, 0.5),
		autoRouteTestGroup("expensive", "expensive-channel", 12, 1.5),
	}).Error)
	require.NoError(t, db.Create([]marketplaceschema.AutoRoutePoolMember{
		{OwnerUserID: 20, GroupID: "expensive", Priority: 1},
		{OwnerUserID: 20, GroupID: "cheap", Priority: 2},
	}).Error)

	bindings, err := ResolveAutoRouteBindings(20, "gpt-5", 1)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.Equal(t, "cheap", bindings[0].GroupID)

	_, err = ResolveAutoRouteBindings(20, "gpt-5", 0.25)
	require.EqualError(t, err, "Auto 路由池中支持该模型的分组倍率均超过 API Key 上限 0.25x")
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
	require.EqualError(t, err, "路由池包含不可用或无权访问的分组")
}

func TestAutoRoutePoolAllowsInvitedPrivateGroup(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.GroupAccess{},
		&marketplaceschema.RankingSnapshot{},
		&marketplaceschema.AutoRoutePoolMember{},
	))
	internalChannelID := 301
	require.NoError(t, db.Create(&marketplaceschema.Channel{
		ID: "invited-channel", OwnerUserID: 11, ProviderType: "openai",
		DeclaredModels: `["gpt-5"]`, InternalChannelID: &internalChannelID,
	}).Error)
	group := autoRouteTestGroup("invited-private", "invited-channel", 11, 0.8)
	group.Visibility = marketplacedomain.VisibilityPrivate
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&marketplaceschema.GroupAccess{
		GroupID: group.ID, UserID: 20, GrantedByInvite: 1,
	}).Error)

	view, err := ReplaceAutoRoutePool(20, AutoRoutePoolUpdateRequest{GroupIDs: []string{group.ID}})
	require.NoError(t, err)
	require.Equal(t, 1, view.SelectedCount)
	require.Equal(t, group.ID, view.Items[0].GroupID)
}

func TestUpdateRoutePoolWithoutConfigPreservesExistingStrategy(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.RoutePool{},
		&marketplaceschema.RoutePoolMember{},
		&marketplaceschema.RankingSnapshot{},
	))
	channelID := 401
	require.NoError(t, db.Create(&marketplaceschema.Channel{
		ID: "pool-channel", OwnerUserID: 11, ProviderType: "openai",
		DeclaredModels: `["gpt-5"]`, InternalChannelID: &channelID,
		Status: marketplacedomain.LifecycleActive,
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Group{
		ID: "pool-group", ChannelID: "pool-channel", OwnerUserID: 11,
		PublicSlug: "pool-group", SystemDisplayName: "pool-group",
		InternalGroupName: "market-pool-group", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1,
		LifecycleStatus:    marketplacedomain.LifecycleActive,
		VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility:         marketplacedomain.VisibilityPublic,
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.RoutePool{
		ID: "pool-1", OwnerUserID: 20, Name: "pool", Strategy: "score",
		MaxAttempts: 3, FailureCooldownSeconds: 30,
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.RoutePoolMember{PoolID: "pool-1", GroupID: "pool-group", Priority: 1}).Error)

	view, err := UpdateRoutePool(20, "pool-1", RoutePoolUpdateRequest{
		Name: "renamed", GroupIDs: []string{"pool-group"},
	})
	require.NoError(t, err)
	require.Equal(t, "renamed", view.Name)
	require.Equal(t, "score", view.Config.Strategy)
}

func TestListRoutePoolModelsReturnsSortedCaseInsensitiveUnion(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{}, &marketplaceschema.Group{},
		&marketplaceschema.RoutePool{}, &marketplaceschema.RoutePoolMember{},
		&marketplaceschema.RankingSnapshot{},
	))
	channelA, channelB := 402, 403
	require.NoError(t, db.Create([]marketplaceschema.Channel{
		{ID: "models-a", OwnerUserID: 11, ProviderType: "openai", DeclaredModels: `["gpt-5","claude-3-7-sonnet"]`, InternalChannelID: &channelA, Status: marketplacedomain.LifecycleActive},
		{ID: "models-b", OwnerUserID: 12, ProviderType: "google", DeclaredModels: `["GPT-5","gemini-2.5"]`, InternalChannelID: &channelB, Status: marketplacedomain.LifecycleActive},
	}).Error)
	groups := []marketplaceschema.Group{
		{ID: "models-group-a", ChannelID: "models-a", OwnerUserID: 11, PublicSlug: "models-a", SystemDisplayName: "models-a", InternalGroupName: "market-models-a", SourceType: marketplacedomain.SourceTypeMarketplaceUser, CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1, LifecycleStatus: marketplacedomain.LifecycleActive, VerificationStatus: marketplacedomain.VerificationPassed, Visibility: marketplacedomain.VisibilityPublic},
		{ID: "models-group-b", ChannelID: "models-b", OwnerUserID: 12, PublicSlug: "models-b", SystemDisplayName: "models-b", InternalGroupName: "market-models-b", SourceType: marketplacedomain.SourceTypeMarketplaceUser, CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1, LifecycleStatus: marketplacedomain.LifecycleActive, VerificationStatus: marketplacedomain.VerificationPassed, Visibility: marketplacedomain.VisibilityPublic},
	}
	require.NoError(t, db.Create(&groups).Error)
	require.NoError(t, db.Create(&marketplaceschema.RoutePool{ID: "models-pool", OwnerUserID: 20, Name: "models", Strategy: "priority", MaxAttempts: 3, FailureCooldownSeconds: 30}).Error)
	require.NoError(t, db.Create([]marketplaceschema.RoutePoolMember{{PoolID: "models-pool", GroupID: groups[0].ID, Priority: 1}, {PoolID: "models-pool", GroupID: groups[1].ID, Priority: 2}}).Error)

	models, err := ListRoutePoolModels(20, "models-pool")
	require.NoError(t, err)
	require.Equal(t, []string{"GPT-5", "claude-3-7-sonnet", "gemini-2.5"}, models)
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
