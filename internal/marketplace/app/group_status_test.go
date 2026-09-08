package app

import (
	"fmt"
	"testing"
	"time"

	gatewaygroups "github.com/sh2001sh/new-api/internal/gateway/groupsettings"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMarketplaceGroupStatusReturnsAllVisibleGroupsBeyondFirstPage(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &marketplaceschema.Channel{}, &marketplaceschema.GroupAccess{}, &marketplaceschema.RankingSnapshot{}, &gatewayschema.Ability{}, &gatewayschema.Channel{}))
	require.NoError(t, db.AutoMigrate(&identityschema.User{}))
	require.NoError(t, db.Create(&identityschema.User{Id: 10, Username: "status-viewer", Group: "default"}).Error)
	for i := 0; i < 127; i++ {
		id := fmt.Sprintf("status-%03d", i)
		group := marketplaceschema.Group{ID: id, ChannelID: id, PublicSlug: id, InternalGroupName: id,
			Visibility: "public", LifecycleStatus: "active", VerificationStatus: "passed", OwnerUserID: 20, Multiplier: 1}
		if i == 125 {
			group.Visibility = "private"
		}
		if i == 126 {
			group.LifecycleStatus = "disabled"
		}
		require.NoError(t, db.Create(&group).Error)
		require.NoError(t, db.Create(&marketplaceschema.Channel{ID: id, DeclaredModels: `["gpt-5.6"]`}).Error)
		require.NoError(t, db.Create(&marketplaceschema.RankingSnapshot{
			GroupID: id, WindowHours: 24, RankingVersion: rankingVersion, CalculatedAt: time.Now().UTC(),
			RequestCount: 20, RawSuccessRate: 95, CacheHitRate: 80,
		}).Error)
	}
	items, err := ListMarketplaceGroupStatus(0)
	require.NoError(t, err)
	require.Len(t, items, 125)
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		ids[item.ID] = true
		require.Equal(t, float64(95), item.SuccessRate)
		require.Equal(t, []string{"gpt-5.6"}, item.Models)
	}
	require.True(t, ids["status-124"], "the third page must be included in the same response")
	require.False(t, ids["status-125"], "private groups must remain private")
	require.False(t, ids["status-126"], "disabled groups must stay excluded")
	require.NoError(t, db.Create(&marketplaceschema.GroupAccess{GroupID: "status-125", UserID: 10}).Error)
	items, err = ListMarketplaceGroupStatus(10)
	require.NoError(t, err)
	require.Len(t, items, 126, "invited users can see their private group")
	t.Log("125 public groups returned in one response; invited viewer receives 126")
}

func TestMarketplaceGroupStatusEmpty(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &gatewayschema.Ability{}, &gatewayschema.Channel{}))
	items, err := ListMarketplaceGroupStatus(0)
	require.NoError(t, err)
	require.NotNil(t, items)
	require.Empty(t, items)
}

func TestMarketplaceStatusIncludesOfficialGroupsWithoutMarketplaceRows(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	originalGroups := gatewaygroups.UserUsableGroups2JSONString()
	originalLogDB := platformdb.LogDB
	platformdb.LogDB = nil
	t.Cleanup(func() {
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(originalGroups))
		platformdb.LogDB = originalLogDB
	})
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"official-visible":"Official","market-pending":"Market","offline":"Offline"}`))
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &gatewayschema.Ability{}, &gatewayschema.Channel{}))
	require.NoError(t, db.Create(&gatewayschema.Channel{Id: 901, Status: 1}).Error)
	require.NoError(t, db.Create(&gatewayschema.Channel{Id: 902, Status: 2}).Error)
	require.NoError(t, db.Create(&[]gatewayschema.Ability{
		{Group: "official-visible", Model: "model-a", ChannelId: 901, Enabled: true},
		{Group: "official-visible", Model: "disabled-model", ChannelId: 901, Enabled: false},
		{Group: "restricted", Model: "secret-model", ChannelId: 901, Enabled: true},
		{Group: "market-pending", Model: "market-model", ChannelId: 901, Enabled: true},
		{Group: "offline", Model: "offline-model", ChannelId: 902, Enabled: true},
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Group{ID: "pending", PublicSlug: "pending", InternalGroupName: "market-pending", SourceType: "marketplace_user", Visibility: "public", LifecycleStatus: "pending_review", VerificationStatus: "passed"}).Error)
	items, err := ListMarketplaceGroupStatus(0)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "official-visible", items[0].SystemDisplayName)
	require.Equal(t, "official", items[0].SourceType)
	require.Equal(t, []string{"model-a"}, items[0].Models)
	require.Equal(t, "unknown", items[0].LatestRequestStatus)
	require.NotNil(t, items[0].ModelVerificationResults)
	models, err := GetMarketplaceGroupModelStatus(items[0].PublicSlug, 0)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "model-a", models[0].Model)
	require.Len(t, models[0].RecentRequestSeries, 24)
	require.NoError(t, db.Create(&marketplaceschema.Group{ID: "official-metadata", ChannelID: "official-metadata", PublicSlug: "official-metadata", InternalGroupName: "official-visible", SourceType: "official", Visibility: "public", LifecycleStatus: "active", VerificationStatus: "passed"}).Error)
	items, err = ListMarketplaceGroupStatus(0)
	require.NoError(t, err)
	require.Len(t, items, 1, "official metadata must not duplicate the gateway group")
	for _, name := range []string{"restricted", "market-pending", "offline"} {
		_, err = GetMarketplaceGroupModelStatus("official:"+name, 0)
		require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	}
}
