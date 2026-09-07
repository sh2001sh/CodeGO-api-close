package app

import (
	"testing"

	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGroupModelStatusKeepsModelsAndGroupsSeparate(t *testing.T) {
	rows := []gatewaystore.GroupModelRequestBucket{
		{GroupName: "group-a", ModelName: "good", BucketIndex: 23, RequestCount: 10, SuccessCount: 10},
		{GroupName: "group-a", ModelName: "bad", BucketIndex: 23, RequestCount: 8, SuccessCount: 2},
		{GroupName: "group-a", ModelName: "bad", BucketIndex: 22, RequestCount: 2, SuccessCount: 2},
		{GroupName: "group-b", ModelName: "good", BucketIndex: 23, RequestCount: 100, SuccessCount: 0},
		{GroupName: "group-a", ModelName: "removed", BucketIndex: 23, RequestCount: 100, SuccessCount: 0},
		{GroupName: "group-a", ModelName: "good", BucketIndex: 24, RequestCount: 100, SuccessCount: 0},
	}
	items := buildGroupModelRequestStatus(1000, "group-a", []string{"good", "bad", "idle", "good"}, rows)
	require.Len(t, items, 3)
	require.EqualValues(t, 10, items[0].RequestCount)
	require.EqualValues(t, 100, items[0].SuccessRate)
	require.EqualValues(t, 40, items[1].SuccessRate, "overall rate must be weighted by requests")
	require.EqualValues(t, 25, items[1].RecentRequestSeries[23].SuccessRate)
	require.Zero(t, items[2].RequestCount)
	require.Len(t, items[2].RecentRequestSeries, 24, "models without requests need an empty status strip")
	require.EqualValues(t, 1000+23*900, items[2].RecentRequestSeries[23].Ts)
}

func TestGroupModelStatusEnforcesVisibilityBeforeStatistics(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &marketplaceschema.Channel{}, &marketplaceschema.GroupAccess{}))
	require.NoError(t, db.Create(&marketplaceschema.Group{ID: "private-model-status", ChannelID: "private-model-status", PublicSlug: "private-model-status", InternalGroupName: "private-model-status", Visibility: "private", LifecycleStatus: "active", OwnerUserID: 42}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Channel{ID: "private-model-status", DeclaredModels: `["model-a","model-b"]`}).Error)
	_, err := GetMarketplaceGroupModelStatus("private-model-status", 0)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = GetMarketplaceGroupModelStatus("private-model-status", 43)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	items, err := GetMarketplaceGroupModelStatus("private-model-status", 42)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.NoError(t, db.Create(&marketplaceschema.GroupAccess{GroupID: "private-model-status", UserID: 43}).Error)
	_, err = GetMarketplaceGroupModelStatus("private-model-status", 43)
	require.NoError(t, err)
	require.NoError(t, db.Model(&marketplaceschema.Group{}).Where("id = ?", "private-model-status").Update("lifecycle_status", "disabled").Error)
	_, err = GetMarketplaceGroupModelStatus("private-model-status", 42)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
