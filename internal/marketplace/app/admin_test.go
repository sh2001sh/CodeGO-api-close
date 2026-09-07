package app

import (
	"testing"
	"time"

	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestListAdminChannelsIncludesOwnerAndFiltersEarningsByTime(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&identityschema.User{},
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.Settlement{},
	))
	require.NoError(t, db.Create(&identityschema.User{Id: 42, ExternalId: "ABC123", Username: "owner-42"}).Error)

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
		OwnerSearch:    "abc",
		StartTimestamp: reference.Add(-24 * time.Hour).Unix(),
		EndTimestamp:   reference.Unix(),
	})
	require.NoError(t, err)
	require.Len(t, channels, 1)
	require.Equal(t, 42, channels[0].OwnerUserID)
	require.Equal(t, "ABC123", channels[0].OwnerExternalID)
	require.EqualValues(t, 2, channels[0].RequestCount)
	require.EqualValues(t, 500, channels[0].TotalIncome)
	require.EqualValues(t, 200, channels[0].PendingIncome)
	require.EqualValues(t, 300, channels[0].ReleasedIncome)
}

func TestListAdminOwnerIncomeKeepsDeletedChannelHistory(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &marketplaceschema.Settlement{}))
	require.NoError(t, db.Create([]identityschema.User{
		{Id: 42, ExternalId: "ABC123", Username: "owner-42", AffCode: "owner42"},
		{Id: 77, ExternalId: "XYZ789", Username: "owner-77", AffCode: "owner77"},
	}).Error)
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
	require.Equal(t, "ABC123", result.Items[0].OwnerExternalID)
	require.Equal(t, 77, result.Items[1].OwnerUserID)
	require.Equal(t, "XYZ789", result.Items[1].OwnerExternalID)

	filtered, err := ListAdminOwnerIncome(AdminOwnerIncomeQuery{
		OwnerSearch:    "abc",
		StartTimestamp: reference.Add(-24 * time.Hour).Unix(),
		EndTimestamp:   reference.Unix(),
	})
	require.NoError(t, err)
	require.Equal(t, 1, filtered.OwnerCount)
	require.Equal(t, "ABC123", filtered.Items[0].OwnerExternalID)
	require.EqualValues(t, 500, filtered.TotalIncome)
}

func TestPartialReclaimIsConsistentAcrossAdminChannelAndOwnerLogSummaries(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &marketplaceschema.Settlement{}))
	require.NoError(t, db.Create(&identityschema.User{Id: 42, ExternalId: "ABC123", Username: "owner-42"}).Error)
	require.NoError(t, db.Create([]marketplaceschema.Settlement{
		{ID: "partial", RequestID: "partial", GroupID: "g", OwnerUserID: 42, OwnerNetAmount: 95, ReclaimedAmount: 40, Status: "released"},
		{ID: "legacy-full", RequestID: "legacy-full", GroupID: "g", OwnerUserID: 42, OwnerNetAmount: 20, Status: "reclaimed"},
	}).Error)
	admin, err := ListAdminOwnerIncome(AdminOwnerIncomeQuery{OwnerSearch: "ABC123"})
	require.NoError(t, err)
	require.EqualValues(t, 115, admin.TotalIncome)
	require.EqualValues(t, 55, admin.ReleasedIncome)
	require.EqualValues(t, 60, admin.ReclaimedIncome)
	require.EqualValues(t, 2, admin.RequestCount)
	channels, err := earningsByGroupIDs([]string{"g"})
	require.NoError(t, err)
	require.Equal(t, admin.ReleasedIncome, channels["g"].ReleasedIncome)
	require.Equal(t, admin.ReclaimedIncome, channels["g"].ReclaimedIncome)
	totals, err := loadOwnerUsageSettlementSummary(42, nil, []string{"g"}, OwnerUsageLogQuery{})
	require.NoError(t, err)
	require.Equal(t, admin.ReleasedIncome, totals.ReleasedIncome)
	require.Equal(t, admin.ReclaimedIncome, totals.ReclaimedIncome)
}

func TestIncomeReclaimSelectionCannotEscapeOwnerSearch(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&identityschema.User{}))
	require.NoError(t, db.Create([]identityschema.User{
		{Id: 42, ExternalId: "ABC123", Username: "wanted", AffCode: "wanted"},
		{Id: 77, ExternalId: "XYZ789", Username: "other", AffCode: "other"},
	}).Error)
	result, err := ReleaseAdminOwnerIncome(AdminOwnerIncomeQuery{OwnerSearch: "ABC123", OwnerUserIDs: []int{77}, MaxAmount: 40})
	require.NoError(t, err)
	require.Zero(t, result.ReclaimedAmount)
}

func TestAdminStatisticsCacheInvalidatesAfterSettlementChanges(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	invalidateAdminMarketplaceStatsCache()
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &marketplaceschema.Settlement{}))
	require.NoError(t, db.Create(&identityschema.User{Id: 42, ExternalId: "ABC123", Username: "owner-42"}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{
		ID: "cached-first", RequestID: "cached-first", GroupID: "cached-group", OwnerUserID: 42,
		OwnerNetAmount: 100, Status: "released",
	}).Error)

	ownerIncome, err := ListAdminOwnerIncome(AdminOwnerIncomeQuery{OwnerSearch: "ABC123"})
	require.NoError(t, err)
	require.EqualValues(t, 100, ownerIncome.TotalIncome)
	groupIncome, err := earningsByGroupIDs([]string{"cached-group"})
	require.NoError(t, err)
	require.EqualValues(t, 100, groupIncome["cached-group"].TotalIncome)

	require.NoError(t, db.Create(&marketplaceschema.Settlement{
		ID: "cached-second", RequestID: "cached-second", GroupID: "cached-group", OwnerUserID: 42,
		OwnerNetAmount: 200, Status: "released",
	}).Error)
	ownerIncome, err = ListAdminOwnerIncome(AdminOwnerIncomeQuery{OwnerSearch: "ABC123"})
	require.NoError(t, err)
	require.EqualValues(t, 100, ownerIncome.TotalIncome)
	groupIncome, err = earningsByGroupIDs([]string{"cached-group"})
	require.NoError(t, err)
	require.EqualValues(t, 100, groupIncome["cached-group"].TotalIncome)

	invalidateAdminMarketplaceStatsCache()
	ownerIncome, err = ListAdminOwnerIncome(AdminOwnerIncomeQuery{OwnerSearch: "ABC123"})
	require.NoError(t, err)
	require.EqualValues(t, 300, ownerIncome.TotalIncome)
	groupIncome, err = earningsByGroupIDs([]string{"cached-group"})
	require.NoError(t, err)
	require.EqualValues(t, 300, groupIncome["cached-group"].TotalIncome)
}

func TestAdminChannelListCacheInvalidatesAfterChannelChanges(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	invalidateAdminMarketplaceStatsCache()
	require.NoError(t, db.AutoMigrate(
		&identityschema.User{},
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.Settlement{},
	))
	require.NoError(t, db.Create(&identityschema.User{Id: 42, ExternalId: "ABC123", Username: "owner-42"}).Error)
	channel := marketplaceschema.Channel{ID: "cached-channel", OwnerUserID: 42, ProviderType: "openai"}
	group := autoRouteTestGroup("cached-group", channel.ID, channel.OwnerUserID, 1)
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{
		ID: "cached-list-first", RequestID: "cached-list-first", GroupID: group.ID, OwnerUserID: 42,
		OwnerNetAmount: 100, Status: "released",
	}).Error)

	channels, err := ListAdminChannels(AdminChannelQuery{OwnerSearch: "ABC123"})
	require.NoError(t, err)
	require.Len(t, channels, 1)
	require.EqualValues(t, 100, channels[0].TotalIncome)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{
		ID: "cached-list-second", RequestID: "cached-list-second", GroupID: group.ID, OwnerUserID: 42,
		OwnerNetAmount: 200, Status: "released",
	}).Error)

	channels, err = ListAdminChannels(AdminChannelQuery{OwnerSearch: "ABC123"})
	require.NoError(t, err)
	require.EqualValues(t, 100, channels[0].TotalIncome)
	invalidateAdminMarketplaceStatsCache()
	channels, err = ListAdminChannels(AdminChannelQuery{OwnerSearch: "ABC123"})
	require.NoError(t, err)
	require.EqualValues(t, 300, channels[0].TotalIncome)
}
