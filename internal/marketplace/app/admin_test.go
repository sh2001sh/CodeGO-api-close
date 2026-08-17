package app

import (
	"testing"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestListAdminChannelsIncludesOwnerAndFiltersEarningsByTime(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.Settlement{},
	))

	channel := marketplaceschema.Channel{
		ID: "admin-income-channel", OwnerUserID: 42, ProviderType: "openai",
		Status: marketplacedomain.LifecycleActive,
	}
	group := autoRouteTestGroup("admin-income-group", channel.ID, channel.OwnerUserID, 1)
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)

	reference := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, db.Create([]marketplaceschema.Settlement{
		{RequestID: "outside", GroupID: group.ID, OwnerUserID: 42, OwnerNetAmount: 100, Status: "released", CreatedAt: reference.Add(-48 * time.Hour)},
		{RequestID: "pending", GroupID: group.ID, OwnerUserID: 42, OwnerNetAmount: 200, Status: "pending", CreatedAt: reference.Add(-2 * time.Hour)},
		{RequestID: "released", GroupID: group.ID, OwnerUserID: 42, OwnerNetAmount: 300, Status: "released", CreatedAt: reference.Add(-time.Hour)},
	}).Error)

	channels, err := ListAdminChannels(AdminChannelQuery{
		StartTimestamp: reference.Add(-24 * time.Hour).Unix(),
		EndTimestamp:   reference.Unix(),
	})
	require.NoError(t, err)
	require.Len(t, channels, 1)
	require.Equal(t, 42, channels[0].OwnerUserID)
	require.EqualValues(t, 2, channels[0].RequestCount)
	require.EqualValues(t, 500, channels[0].TotalIncome)
	require.EqualValues(t, 200, channels[0].PendingIncome)
	require.EqualValues(t, 300, channels[0].ReleasedIncome)
}

func TestListAdminOwnerIncomeKeepsDeletedChannelHistory(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Settlement{}))
	reference := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, db.Create([]marketplaceschema.Settlement{
		{RequestID: "deleted-channel-pending", GroupID: "deleted-group", OwnerUserID: 42, OwnerNetAmount: 200, Status: "pending", CreatedAt: reference.Add(-2 * time.Hour)},
		{RequestID: "deleted-channel-released", GroupID: "deleted-group", OwnerUserID: 42, OwnerNetAmount: 300, Status: "released", CreatedAt: reference.Add(-time.Hour)},
		{RequestID: "other-owner", GroupID: "other-deleted-group", OwnerUserID: 77, OwnerNetAmount: 400, Status: "released", CreatedAt: reference.Add(-time.Hour)},
		{RequestID: "outside-range", GroupID: "deleted-group", OwnerUserID: 42, OwnerNetAmount: 100, Status: "released", CreatedAt: reference.Add(-48 * time.Hour)},
	}).Error)

	result, err := ListAdminOwnerIncome(AdminOwnerIncomeQuery{
		StartTimestamp: reference.Add(-24 * time.Hour).Unix(),
		EndTimestamp:   reference.Unix(),
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.OwnerCount)
	require.EqualValues(t, 3, result.RequestCount)
	require.EqualValues(t, 900, result.TotalIncome)
	require.EqualValues(t, 200, result.PendingIncome)
	require.EqualValues(t, 700, result.ReleasedIncome)
	require.Equal(t, 42, result.Items[0].OwnerUserID)
	require.Equal(t, 77, result.Items[1].OwnerUserID)
}
