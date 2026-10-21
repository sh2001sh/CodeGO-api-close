package app

import (
	"fmt"
	"strings"
	"testing"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestKeyGroupOptionsAccessPoolsAndBoundedQueries(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	// No ranking, log, request-series or verification tables: selectors must
	// continue to work independently of the analytics subsystem.
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &marketplaceschema.Channel{},
		&marketplaceschema.GroupAccess{}, &marketplaceschema.ChannelUserBlock{}, &marketplaceschema.UserMultiplier{},
		&marketplaceschema.RoutePool{}, &marketplaceschema.RoutePoolMember{}, &marketplaceschema.AutoRoutePoolMember{}))
	const userID = 10
	channelID := 1
	addGroup := func(id, visibility, status, verification string, owner int) {
		require.NoError(t, db.Create(&marketplaceschema.Channel{
			ID: id, OwnerUserID: owner, InternalChannelID: &channelID, DeclaredModels: `["gpt-5.6"]`,
			SubmittedSourceLabel: "Test", ApprovedSourceLabel: "旧来源", SourceLabelStatus: marketplacedomain.SourceLabelDeclared,
		}).Error)
		require.NoError(t, db.Create(&marketplaceschema.Group{
			ID: id, ChannelID: id, OwnerUserID: owner, PublicSlug: id, InternalGroupName: id,
			SourceType: marketplacedomain.SourceTypeMarketplaceUser, Visibility: visibility,
			LifecycleStatus: status, VerificationStatus: verification, Multiplier: 2,
			CreditPoolPolicy: marketplacedomain.CreditPolicySubscriptionAndUniversal,
		}).Error)
	}
	addGroup("public", "public", "active", "passed", 20)
	addGroup("invited", "private", "degraded", "passed", 20)
	addGroup("owned", "private", "active", "passed", userID)
	addGroup("hidden", "private", "active", "passed", 20)
	addGroup("disabled", "public", "disabled", "passed", 20)
	addGroup("suspended", "public", "suspended", "passed", 20)
	addGroup("unverified", "public", "active", "pending", 20)
	addGroup("blocked", "public", "active", "passed", 20)
	addGroup("deleted", "public", "active", "passed", 20)
	require.NoError(t, db.Delete(&marketplaceschema.Group{}, "id = ?", "deleted").Error)
	require.NoError(t, db.Create(&marketplaceschema.GroupAccess{GroupID: "invited", UserID: userID}).Error)
	require.NoError(t, db.Create(&marketplaceschema.ChannelUserBlock{ChannelID: "blocked", UserID: userID}).Error)
	require.NoError(t, db.Create(&marketplaceschema.UserMultiplier{ChannelID: "public", UserID: userID, Multiplier: 0.7}).Error)
	require.NoError(t, db.Create(&marketplaceschema.UserMultiplier{ChannelID: "owned", UserID: userID, Multiplier: 0.1}).Error)
	require.NoError(t, db.Create(&marketplaceschema.RoutePool{ID: "mine", Name: "GPT 工作池", OwnerUserID: userID}).Error)
	require.NoError(t, db.Create(&marketplaceschema.RoutePool{ID: "other", Name: "Private pool", OwnerUserID: 20}).Error)
	for _, id := range []string{"public", "invited", "hidden", "deleted", "missing", "blocked"} {
		require.NoError(t, db.Create(&marketplaceschema.RoutePoolMember{PoolID: "mine", GroupID: id}).Error)
	}
	require.NoError(t, db.Create(&marketplaceschema.AutoRoutePoolMember{OwnerUserID: userID, GroupID: "public"}).Error)
	queryCount := 0
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("count_selector_queries", func(tx *gorm.DB) {
		queryCount++
		sql := strings.ToLower(tx.Statement.SQL.String())
		require.NotContains(t, sql, "ciphertext")
		require.NotContains(t, sql, "select * from `marketplace_channels`")
	}))
	options, err := ListKeyGroupOptions(userID)
	require.NoError(t, err)
	byValue := make(map[string]KeyGroupOption)
	for _, option := range options {
		byValue[option.Value] = option
	}
	require.Len(t, options, 5)
	require.Equal(t, "GPT 工作池", byValue["market:pool:mine"].Label)
	require.Equal(t, 2, byValue["market:pool:mine"].MemberCount)
	require.Equal(t, []string{"gpt-5.6"}, byValue["market:pool:mine"].Models)
	require.Equal(t, 1, byValue["market:auto"].MemberCount)
	require.Equal(t, 0.7, *byValue["market:public"].Multiplier)
	require.Equal(t, 2.0, *byValue["market:owned"].Multiplier)
	require.True(t, byValue["market:public"].SubscriptionEnabled)
	require.Equal(t, "public-Test-2x", byValue["market:public"].Label)
	baselineQueries := queryCount
	for i := 0; i < 1005; i++ {
		addGroup(fmt.Sprintf("more-%d", i), "public", "active", "passed", 20)
	}
	queryCount = 0
	options, err = ListKeyGroupOptions(userID)
	require.NoError(t, err)
	require.Len(t, options, 1010, "selectors must not inherit the 1000-row marketplace discovery cap")
	require.Equal(t, baselineQueries, queryCount, "query count must not grow with group count")
	t.Logf("3 and 1008 selectable groups: %d queries each, without analytics tables", queryCount)
	_, err = ListKeyGroupOptions(0)
	require.Error(t, err)
	// A real storage failure must not be reported as an empty successful list.
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("fail_selector_queries", func(tx *gorm.DB) {
		tx.AddError(fmt.Errorf("database unavailable"))
	}))
	_, err = ListKeyGroupOptions(userID)
	require.ErrorContains(t, err, "database unavailable")
}

func TestKeyPoolOptionPreservesEmptyPoolAndOfficialModels(t *testing.T) {
	option := keyPoolOption("market:pool:mine", "My pool", "marketplace_pool", []string{"official:vip", "market", "missing", "market"}, map[string][]string{
		"official:vip": {"gpt-5.6", "claude"}, "market": {"gpt-5.6"},
	})
	require.Equal(t, 2, option.MemberCount)
	require.Equal(t, []string{"claude", "gpt-5.6"}, option.Models)
	empty := keyPoolOption("market:pool:empty", "Empty pool", "marketplace_pool", []string{"deleted"}, nil)
	require.Equal(t, "Empty pool", empty.Label)
	require.Zero(t, empty.MemberCount)
	require.Empty(t, empty.Models)
	require.NotNil(t, empty.Models)
}

func TestKeyGroupOptionsKeepViewerPricesAndAccessFresh(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.GroupAccess{},
		&marketplaceschema.ChannelUserBlock{}, &marketplaceschema.RoutePool{}, &marketplaceschema.RoutePoolMember{},
		&marketplaceschema.AutoRoutePoolMember{}, &marketplaceschema.RankingSnapshot{}))
	for _, id := range []string{"public", "invited"} {
		visibility := marketplacedomain.VisibilityPublic
		if id == "invited" {
			visibility = marketplacedomain.VisibilityPrivate
		}
		require.NoError(t, db.Create(&marketplaceschema.Channel{ID: id, OwnerUserID: 30, DeclaredModels: `["model"]`}).Error)
		require.NoError(t, db.Create(&marketplaceschema.Group{
			ID: id, ChannelID: id, OwnerUserID: 30, PublicSlug: id, InternalGroupName: id,
			SourceType: marketplacedomain.SourceTypeMarketplaceUser, Visibility: visibility,
			LifecycleStatus: marketplacedomain.LifecycleActive, VerificationStatus: marketplacedomain.VerificationPassed, Multiplier: 2,
		}).Error)
		require.NoError(t, db.Create(&marketplaceschema.RankingSnapshot{GroupID: id, WindowHours: 24, RankingVersion: rankingVersion,
			RawSuccessRate: 98.5, AvgTTFTMs: 42, RequestCount: 100,
			AvgConsumerAmountByModel: strings.Repeat("heavy-private-unused-field", 1000),
		}).Error)
	}
	require.NoError(t, db.Create(&marketplaceschema.GroupAccess{GroupID: "invited", UserID: 10}).Error)
	require.NoError(t, db.Create(&marketplaceschema.UserMultiplier{ChannelID: "public", UserID: 10, Multiplier: 0.7}).Error)
	require.NoError(t, db.Create(&marketplaceschema.UserMultiplier{ChannelID: "public", UserID: 11, Multiplier: 1.3}).Error)
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("narrow_key_snapshots", func(tx *gorm.DB) {
		if tx.Statement.Table == (marketplaceschema.RankingSnapshot{}).TableName() {
			require.Equal(t, []string{"group_id, raw_success_rate, avg_ttft_ms, request_count"}, tx.Statement.Selects)
		}
	}))
	read := func(userID int) map[string]KeyGroupOption {
		t.Helper()
		options, err := ListKeyGroupOptions(userID)
		require.NoError(t, err)
		result := make(map[string]KeyGroupOption, len(options))
		for _, option := range options {
			result[option.Value] = option
		}
		return result
	}
	first, second := read(10), read(11)
	require.Equal(t, 0.7, *first["market:public"].Multiplier)
	require.Equal(t, 1.3, *second["market:public"].Multiplier)
	require.Contains(t, first, "market:invited")
	require.NotContains(t, second, "market:invited")
	require.Equal(t, 98.5, *first["market:public"].SuccessRate)
	require.Equal(t, 42.0, *first["market:public"].AvgTTFTMs)
	require.EqualValues(t, 100, first["market:public"].RequestCount)
	// Completed loads must never cache permission, price or health changes.
	require.NoError(t, db.Model(&marketplaceschema.UserMultiplier{}).Where("user_id = ?", 10).Update("multiplier", 0.9).Error)
	require.NoError(t, db.Delete(&marketplaceschema.GroupAccess{}, "user_id = ?", 10).Error)
	require.NoError(t, db.Model(&marketplaceschema.RankingSnapshot{}).Where("group_id = ?", "public").Update("raw_success_rate", 75.0).Error)
	updated := read(10)
	require.Equal(t, 0.9, *updated["market:public"].Multiplier)
	require.NotContains(t, updated, "market:invited")
	require.Equal(t, 75.0, *updated["market:public"].SuccessRate)
	require.NoError(t, db.Create(&marketplaceschema.ChannelUserBlock{ChannelID: "public", UserID: 10}).Error)
	require.NotContains(t, read(10), "market:public")
	require.Contains(t, read(11), "market:public")
}
