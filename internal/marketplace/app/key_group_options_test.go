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
			ApprovedSourceLabel: "Test", SourceLabelStatus: marketplacedomain.SourceLabelApproved,
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
	baselineQueries := queryCount
	for i := 0; i < 65; i++ {
		addGroup(fmt.Sprintf("more-%d", i), "public", "active", "passed", 20)
	}
	queryCount = 0
	options, err = ListKeyGroupOptions(userID)
	require.NoError(t, err)
	require.Len(t, options, 70, "all options must arrive in one response, without 50-row pagination")
	require.Equal(t, baselineQueries, queryCount, "query count must not grow with group count")
	t.Logf("3 and 68 selectable groups: %d queries each, without analytics tables", queryCount)
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
