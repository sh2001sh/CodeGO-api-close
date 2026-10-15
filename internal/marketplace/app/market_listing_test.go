package app

import (
	"fmt"
	"testing"
	"time"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewaygroups "github.com/sh2001sh/new-api/internal/gateway/groupsettings"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMarketListingKeepsOfficialOnFirstPageAndSearchesBothSources(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	oldLogDB := platformdb.LogDB
	oldGroups := gatewaygroups.UserUsableGroups2JSONString()
	platformdb.LogDB = nil
	t.Cleanup(func() {
		platformdb.LogDB = oldLogDB
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(oldGroups))
		marketplaceListCache.result = nil
	})
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"official-list":"Official"}`))
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &marketplaceschema.Channel{}, &marketplaceschema.GroupAccess{}, &marketplaceschema.RankingSnapshot{}, &marketplaceschema.ChannelFeedback{}, &gatewayschema.Ability{}, &gatewayschema.Channel{}))
	require.NoError(t, db.Create(&gatewayschema.Channel{Id: 801, Status: 1}).Error)
	require.NoError(t, db.Create(&gatewayschema.Ability{Group: "official-list", Model: "shared-model", ChannelId: 801, Enabled: true}).Error)
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("listed-%02d", i)
		require.NoError(t, db.Create(&marketplaceschema.Group{ID: id, ChannelID: id, PublicSlug: id, InternalGroupName: id, SourceType: "marketplace_user", Visibility: "public", LifecycleStatus: "active", VerificationStatus: "passed"}).Error)
		require.NoError(t, db.Create(&marketplaceschema.Channel{ID: id, DeclaredModels: `["shared-model","third-party-only"]`}).Error)
		require.NoError(t, db.Create(&marketplaceschema.RankingSnapshot{GroupID: id, WindowHours: 24, RankingVersion: rankingVersion, Score: 99, CalculatedAt: time.Now()}).Error)
	}
	for _, page := range []int{1, 2} {
		result, err := ListMarketplaceGroups(GroupQuery{SeparateOfficial: true, Page: page})
		require.NoError(t, err)
		require.Equal(t, 25, result.Total)
		if page == 1 {
			require.Len(t, result.OfficialItems, 1)
			require.Equal(t, "official:official-list", result.OfficialItems[0].ID)
			require.Len(t, result.Items, 20)
		} else {
			require.Empty(t, result.OfficialItems, "official section belongs only on the first page")
			require.Len(t, result.Items, 5)
		}
	}
	for _, tc := range []struct {
		search string
		total  int
	}{
		{"shared-model", 26}, {"third-party-only", 25}, {"official-list", 1}, {"no-match", 0},
	} {
		result, err := ListMarketplaceGroups(GroupQuery{Search: tc.search, PageSize: 50})
		require.NoError(t, err)
		require.Equal(t, tc.total, result.Total, tc.search)
		require.Len(t, result.Items, tc.total)
		require.Empty(t, result.OfficialItems)
	}
}

func TestMarketMultiplierLookupIsBatchedAndIsolatedByViewer(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.UserMultiplier{}))
	require.NoError(t, db.Create(&[]marketplaceschema.UserMultiplier{
		{ChannelID: "channel-0", UserID: 10, Multiplier: 0.25},
		{ChannelID: "channel-1", UserID: 11, Multiplier: 0.1},
		{ChannelID: "channel-2", UserID: 10, Multiplier: 0.1},
	}).Error)
	groups := make([]marketplaceschema.Group, 0, 200)
	channels := make(map[string]marketplaceschema.Channel)
	for i := 0; i < 200; i++ {
		id := fmt.Sprintf("channel-%d", i)
		groups = append(groups, marketplaceschema.Group{ID: id, ChannelID: id, Multiplier: 1, OwnerUserID: 99})
		channels[id] = marketplaceschema.Channel{ID: id}
	}
	groups[2].OwnerUserID = 10
	queries := 0
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:multiplier_queries", func(tx *gorm.DB) {
		if tx.Statement.Table == (marketplaceschema.UserMultiplier{}).TableName() {
			queries++
		}
	}))
	items, err := filterAndSortGroups(groups, channels, nil, nil, GroupQuery{ViewerUserID: 10})
	require.NoError(t, err)
	require.Len(t, items, 200)
	require.Equal(t, 1, queries)
	prices := make(map[string]float64)
	for _, item := range items {
		prices[item.ID] = item.Multiplier
	}
	require.Equal(t, 0.25, prices["channel-0"])
	require.Equal(t, 1.0, prices["channel-1"], "another viewer's price must remain private")
	require.Equal(t, 1.0, prices["channel-2"], "owners retain the listed price")
	require.NoError(t, db.Migrator().DropTable(&marketplaceschema.UserMultiplier{}))
	_, err = filterAndSortGroups(groups, channels, nil, nil, GroupQuery{ViewerUserID: 10})
	require.Error(t, err, "database errors must not silently replace negotiated prices")
	t.Log("200 groups: 1 multiplier query; viewer isolation and failure path verified")
}

func TestOfficialPoolCapabilitiesBatchDeduplicatesAndExcludesDisabledChannels(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}, &gatewayschema.Ability{}))
	require.NoError(t, db.Create(&[]gatewayschema.Channel{
		{Id: 1, Status: 1, MultiplierCardUserEnabled: true},
		{Id: 2, Status: 1}, {Id: 3, Status: 2},
	}).Error)
	require.NoError(t, db.Create(&[]gatewayschema.Ability{
		{Group: "a", Model: "m1", ChannelId: 1, Enabled: true},
		{Group: "a", Model: "m1", ChannelId: 2, Enabled: true},
		{Group: "a", Model: "m2", ChannelId: 1, Enabled: true},
		{Group: "b", Model: "m1", ChannelId: 2, Enabled: true},
		{Group: "a", Model: "disabled", ChannelId: 3, Enabled: true},
		{Group: "a", Model: "disabled-ability", ChannelId: 1, Enabled: false},
		{Group: "private", Model: "private", ChannelId: 1, Enabled: true},
	}).Error)
	groups, err := loadOfficialGroupCapabilities([]string{"a", "b"})
	require.NoError(t, err)
	require.Len(t, groups, 2)
	require.Equal(t, []string{"m1", "m2"}, groups["a"].Models)
	require.Equal(t, []int{1, 2}, groups["a"].ChannelIDs)
	require.True(t, groups["a"].MultiplierCard)
	require.False(t, groups["b"].MultiplierCard)
	metrics := aggregateOfficialGroupMetrics(groups["a"].ChannelIDs, map[int]auditprojection.ChannelSummary{
		1: {RequestCount: 10, SuccessRate: 100},
		2: {RequestCount: 30, SuccessRate: 50},
	}, "healthy")
	require.EqualValues(t, 40, metrics.RequestCount)
	require.Equal(t, 62.5, metrics.SuccessRate)
}
