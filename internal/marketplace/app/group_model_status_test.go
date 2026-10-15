package app

import (
	"testing"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGroupModelStatusKeepsModelsAndGroupsSeparate(t *testing.T) {
	const start = int64(3600)
	rows := []auditprojection.GroupModelSeries{
		{Group: "group-a", ModelName: "good", Series: []auditprojection.BucketPoint{{Ts: start + 5*3600, RequestCount: 10, SuccessRate: 100}}},
		{Group: "group-a", ModelName: "bad", Series: []auditprojection.BucketPoint{
			{Ts: start + 4*3600, RequestCount: 2, SuccessRate: 100},
			{Ts: start + 5*3600, RequestCount: 8, SuccessRate: 25},
		}},
		{Group: "group-b", ModelName: "good", Series: []auditprojection.BucketPoint{{Ts: start + 5*3600, RequestCount: 100, SuccessRate: 0}}},
		{Group: "group-a", ModelName: "removed", Series: []auditprojection.BucketPoint{{Ts: start + 5*3600, RequestCount: 100, SuccessRate: 0}}},
		{Group: "group-a", ModelName: "good", Series: []auditprojection.BucketPoint{{Ts: start + 6*3600, RequestCount: 100, SuccessRate: 0}}},
	}
	items := buildGroupModelRequestStatus(start, "group-a", []string{"good", "bad", "idle", "good"}, rows)
	require.Len(t, items, 3)
	require.EqualValues(t, 10, items[0].RequestCount)
	require.EqualValues(t, 100, items[0].SuccessRate)
	require.EqualValues(t, 40, items[1].SuccessRate, "overall rate must be weighted by requests")
	require.EqualValues(t, 25, items[1].RecentRequestSeries[5].SuccessRate)
	require.Zero(t, items[2].RequestCount)
	require.Len(t, items[2].RecentRequestSeries, 6, "models without requests need an empty status strip")
	require.EqualValues(t, start+5*3600, items[2].RecentRequestSeries[5].Ts)
}

func TestGroupModelStatusEnforcesVisibilityBeforeStatistics(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &marketplaceschema.Channel{}, &marketplaceschema.GroupAccess{}))
	require.NoError(t, db.Create(&marketplaceschema.Group{ID: "private-model-status", ChannelID: "private-model-status", PublicSlug: "private-model-status", InternalGroupName: "private-model-status", Visibility: "private", LifecycleStatus: "active", VerificationStatus: "passed", OwnerUserID: 42}).Error)
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
