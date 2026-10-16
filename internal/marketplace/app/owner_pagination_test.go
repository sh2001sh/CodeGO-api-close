package app

import (
	"errors"
	"fmt"
	"testing"
	"time"

	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOwnerBargainPaginationDoesNotLoseRowsWithEqualTimestamps(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.BargainRequest{}, &identityschema.User{}))
	for _, group := range []marketplaceschema.Group{
		{ID: "own", ChannelID: "c1", PublicSlug: "own", InternalGroupName: "own", OwnerUserID: 10, SystemDisplayName: "Owned group"},
		{ID: "other", ChannelID: "c2", PublicSlug: "other", InternalGroupName: "other", OwnerUserID: 11},
	} {
		require.NoError(t, db.Create(&group).Error)
	}
	require.NoError(t, db.Create(&identityschema.User{Id: 20, Username: "bargain-user", ExternalId: "U20"}).Error)
	created := time.Now().UTC().Truncate(time.Second)
	rows := make([]marketplaceschema.BargainRequest, 0, 63)
	for i := range 61 {
		rows = append(rows, marketplaceschema.BargainRequest{ID: fmt.Sprintf("own-%03d", i), GroupID: "own", UserID: 20, Status: "pending", CreatedAt: created})
	}
	rows = append(rows,
		marketplaceschema.BargainRequest{ID: "other-request", GroupID: "other", UserID: 20, Status: "pending", CreatedAt: created},
		marketplaceschema.BargainRequest{ID: "approved", GroupID: "own", UserID: 20, Status: "approved", CreatedAt: created},
	)
	require.NoError(t, db.Create(&rows).Error)
	seen := make(map[string]bool)
	for page := 1; page <= 4; page++ {
		result, err := ListOwnerBargainRequests(10, map[string]string{"status": "pending"}, page, 20)
		require.NoError(t, err)
		require.EqualValues(t, 61, result["total"])
		items := result["items"].([]BargainRequestItem)
		require.Len(t, items, min(20, 61-(page-1)*20))
		for _, item := range items {
			require.False(t, seen[item.ID], "duplicate across pages: %s", item.ID)
			seen[item.ID] = true
			require.Equal(t, "own", item.GroupID)
			require.Equal(t, "U20", item.UserExternalID)
			require.Equal(t, "Owned group", item.GroupName)
		}
	}
	require.Len(t, seen, 61)
	for _, tc := range []struct{ owner, page int }{{10, 5}, {99, 1}} {
		result, err := ListOwnerBargainRequests(tc.owner, map[string]string{"status": "pending"}, tc.page, 20)
		require.NoError(t, err)
		require.Empty(t, result["items"])
	}
	result, err := ListOwnerBargainRequests(10, map[string]string{"group_id": "other"}, 1, 20)
	require.NoError(t, err)
	require.Zero(t, result["total"])
	result, err = ListOwnerBargainRequests(10, map[string]string{"status": "pending"}, -1, 0)
	require.NoError(t, err)
	require.Equal(t, 1, result["page"])
	require.Equal(t, 20, result["page_size"])
}

func TestOwnerBargainCountFailureIsReturned(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.BargainRequest{}))
	want := errors.New("bargain count unavailable")
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:count_error", func(tx *gorm.DB) {
		_ = tx.AddError(want)
	}))
	_, err := ListOwnerBargainRequests(10, map[string]string{}, 1, 20)
	require.ErrorIs(t, err, want)
}

func TestOwnerMultiplierPaginationCanReadPastFiftyUsers(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &identityschema.User{}))
	require.NoError(t, db.Create(&marketplaceschema.Channel{ID: "own", OwnerUserID: 10}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Group{ID: "own", OwnerUserID: 10, ChannelID: "own", PublicSlug: "own", InternalGroupName: "own"}).Error)
	for i := range 121 {
		require.NoError(t, db.Create(&identityschema.User{Id: i + 100, Username: fmt.Sprintf("user-%d", i), AffCode: fmt.Sprintf("p%d", i)}).Error)
		require.NoError(t, db.Create(&marketplaceschema.UserMultiplier{ChannelID: "own", UserID: i + 100, Multiplier: 0.5}).Error)
	}
	seen := map[int]bool{}
	for page := 1; page <= 3; page++ {
		items, total, err := ListOwnerMultipliers(10, page)
		require.NoError(t, err)
		require.EqualValues(t, 121, total)
		for _, item := range items {
			require.False(t, seen[item.UserID])
			seen[item.UserID] = true
		}
	}
	require.Len(t, seen, 121)
	items, total, err := ListOwnerMultipliers(99, 1)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, items)
}

func TestOwnerUsageCanReadPastTwoHundredUsers(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Settlement{}, &identityschema.User{}))
	require.NoError(t, db.Create(&marketplaceschema.Group{ID: "own", OwnerUserID: 10, ChannelID: "c1", PublicSlug: "own", InternalGroupName: "own"}).Error)
	rows := make([]marketplaceschema.Settlement, 0, 231)
	for i := range 231 {
		rows = append(rows, marketplaceschema.Settlement{ID: fmt.Sprintf("usage-%d", i), RequestID: fmt.Sprintf("usage-%d", i), OwnerUserID: 10, GroupID: "own", ConsumerUserID: 1000 + i, ConsumerAmount: 1, CreatedAt: time.Now().UTC()})
	}
	require.NoError(t, db.Create(&rows).Error)
	seen := map[string]bool{}
	for page := 1; page <= 2; page++ {
		result, err := ListOwnerChannelUserUsage(10, OwnerUserUsageQuery{ChannelID: "c1", Page: page, PageSize: 200})
		require.NoError(t, err)
		require.EqualValues(t, 231, result["total"])
		for _, item := range result["items"].([]OwnerUserUsageItem) {
			require.False(t, seen[item.UserID])
			seen[item.UserID] = true
		}
	}
	require.Len(t, seen, 231)
}
