package app

import (
	"testing"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestOwnerChannelEarningsScopeStatesAndFreshness(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Settlement{}))
	entries := []marketplaceschema.Settlement{
		{ID: "pending", RequestID: "pending", GroupID: "shared", OwnerUserID: 42, OwnerNetAmount: 100, Status: "pending"},
		{ID: "partial", RequestID: "partial", GroupID: "shared", OwnerUserID: 42, OwnerNetAmount: 200, ReclaimedAmount: 60, Status: "released"},
		{ID: "reclaimed", RequestID: "reclaimed", GroupID: "shared", OwnerUserID: 42, OwnerNetAmount: 300, Status: "reclaimed"},
		{ID: "forfeited", RequestID: "forfeited", GroupID: "shared", OwnerUserID: 42, OwnerNetAmount: 400, Status: "forfeited"},
		{ID: "other-owner", RequestID: "other-owner", GroupID: "shared", OwnerUserID: 43, OwnerNetAmount: 9000, Status: "pending"},
		{ID: "other-group", RequestID: "other-group", GroupID: "unrequested", OwnerUserID: 42, OwnerNetAmount: 8000, Status: "pending"},
	}
	require.NoError(t, db.Create(&entries).Error)
	// Populate the broader admin cache before reading the owner scope.
	admin, err := earningsByGroupIDs([]string{"shared"})
	require.NoError(t, err)
	require.EqualValues(t, 10000, admin["shared"].TotalIncome)
	actual, err := earningsByOwnerGroupIDs(42, []string{"shared", "absent"})
	require.NoError(t, err)
	require.Equal(t, map[string]ownerChannelEarnings{"shared": {
		GroupID: "shared", RequestCount: 4, TotalIncome: 1000,
		PendingIncome: 100, ReleasedIncome: 140, ReclaimedIncome: 360, ForfeitedIncome: 400,
	}}, actual)
	foreign, err := earningsByOwnerGroupIDs(43, []string{"shared"})
	require.NoError(t, err)
	require.EqualValues(t, 9000, foreign["shared"].TotalIncome)
	// No explicit admin invalidation: a committed settlement update must appear.
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Where("id = ?", "pending").Updates(map[string]any{
		"owner_net_amount": 150, "status": "released", "reclaimed_amount": 10,
	}).Error)
	fresh, err := earningsByOwnerGroupIDs(42, []string{"absent", "shared"})
	require.NoError(t, err)
	require.EqualValues(t, 1050, fresh["shared"].TotalIncome)
	require.Zero(t, fresh["shared"].PendingIncome)
	require.EqualValues(t, 280, fresh["shared"].ReleasedIncome)
	require.EqualValues(t, 370, fresh["shared"].ReclaimedIncome)
	_, err = earningsByOwnerGroupIDs(0, []string{"shared"})
	require.Error(t, err)
	_, err = ListOwnerChannels(0)
	require.Error(t, err)
	empty, err := earningsByOwnerGroupIDs(42, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestChannelEarningsRangeKeepsInclusiveSecondBoundary(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Settlement{}))
	start := time.Date(2026, 10, 18, 0, 0, 0, 0, time.Local)
	end := start.Add(time.Hour)
	for index, at := range []time.Time{start.Add(-time.Nanosecond), start, end.Add(999 * time.Millisecond), end.Add(time.Second)} {
		id := []string{"before", "start", "end-fraction", "after"}[index]
		require.NoError(t, db.Create(&marketplaceschema.Settlement{
			ID: id, RequestID: id, GroupID: "window", OwnerUserID: 42,
			OwnerNetAmount: 100, Status: "pending", CreatedAt: at,
		}).Error)
	}
	actual, err := earningsByGroupIDsInRange([]string{"window"}, start.Unix(), end.Unix())
	require.NoError(t, err)
	require.EqualValues(t, 2, actual["window"].RequestCount)
	require.EqualValues(t, 200, actual["window"].TotalIncome)
}

func TestOwnerInventoryIncomeFreshAcrossIndexAndDetails(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}, &marketplaceschema.Settlement{}, &marketplaceschema.VerificationRun{}))
	channel := marketplaceschema.Channel{ID: "income-channel", OwnerUserID: 42}
	group := autoRouteTestGroup("income-group", channel.ID, 42, 1)
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)
	// Deleted inventory remains visible to its owner with its historical income.
	require.NoError(t, db.Delete(&channel).Error)
	require.NoError(t, db.Delete(&group).Error)
	for _, amount := range []int64{100, 250} {
		if amount == 100 {
			require.NoError(t, db.Create(&marketplaceschema.Settlement{
				ID: "inventory-income", RequestID: "inventory-income", GroupID: group.ID,
				OwnerUserID: 42, OwnerNetAmount: amount, Status: "pending",
			}).Error)
		} else {
			require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Where("id = ?", "inventory-income").Update("owner_net_amount", amount).Error)
		}
		index, err := ListOwnerChannelIndex(42)
		require.NoError(t, err)
		require.Len(t, index, 1)
		require.Equal(t, amount, index[0].TotalIncome)
		details, err := ListOwnerChannelsByIDs(42, []string{channel.ID, "absent"})
		require.NoError(t, err)
		require.Len(t, details, 1)
		require.Equal(t, amount, details[0].TotalIncome)
		require.Equal(t, amount, details[0].PendingIncome)
		require.EqualValues(t, 1, details[0].RequestCount)
	}
	foreign, err := ListOwnerChannelsByIDs(43, []string{channel.ID})
	require.NoError(t, err)
	require.Empty(t, foreign)
}
