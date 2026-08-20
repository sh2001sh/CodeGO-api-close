package app

import (
	"testing"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestRankingSnapshotsForRequestUsesFreshPersistedRows(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.RankingSnapshot{}))
	now := time.Now().UTC()
	groups := []marketplaceschema.Group{{ID: "group-a"}, {ID: "group-b"}}
	require.NoError(t, db.Create(&[]marketplaceschema.RankingSnapshot{
		{GroupID: "group-a", WindowHours: 24, RankingVersion: rankingVersion, Rank: 1, Score: 90, CalculatedAt: now},
		{GroupID: "group-b", WindowHours: 24, RankingVersion: rankingVersion, Rank: 2, Score: 80, CalculatedAt: now},
	}).Error)

	snapshots, err := rankingSnapshotsForRequest(groups, nil, 24)
	require.NoError(t, err)
	require.Len(t, snapshots, 2)
	require.Equal(t, 90.0, snapshots["group-a"].Score)
}

func TestRankingSnapshotsStaleUsesWindowSpecificFreshness(t *testing.T) {
	now := time.Now().UTC()
	snapshots := map[string]marketplaceschema.RankingSnapshot{
		"group-a": {CalculatedAt: now.Add(-3 * time.Minute)},
	}

	require.True(t, rankingSnapshotsStale(snapshots, 24, now))
	require.False(t, rankingSnapshotsStale(snapshots, 24*7, now))
}

func TestPersistRankingSnapshotsUpsertsBatch(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.RankingSnapshot{}))
	now := time.Now().UTC()
	require.NoError(t, persistRankingSnapshots([]marketplaceschema.RankingSnapshot{
		{GroupID: "group-a", WindowHours: 24, RankingVersion: rankingVersion, Score: 10, CalculatedAt: now},
		{GroupID: "group-b", WindowHours: 24, RankingVersion: rankingVersion, Score: 20, CalculatedAt: now},
	}))
	require.NoError(t, persistRankingSnapshots([]marketplaceschema.RankingSnapshot{
		{GroupID: "group-a", WindowHours: 24, RankingVersion: rankingVersion, Score: 30, CalculatedAt: now.Add(time.Minute)},
		{GroupID: "group-b", WindowHours: 24, RankingVersion: rankingVersion, Score: 40, CalculatedAt: now.Add(time.Minute)},
	}))

	var count int64
	require.NoError(t, db.Model(&marketplaceschema.RankingSnapshot{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
	var stored marketplaceschema.RankingSnapshot
	require.NoError(t, platformdb.DB.First(&stored, "group_id = ?", "group-a").Error)
	require.Equal(t, 30.0, stored.Score)
}

func TestRankingSnapshotsForFilteredPublicRequestRefreshesFullPublicMarket(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{},
		&marketplaceschema.Group{},
		&marketplaceschema.RankingSnapshot{},
	))
	originalLogDB := platformdb.LogDB
	platformdb.LogDB = nil
	t.Cleanup(func() { platformdb.LogDB = originalLogDB })

	channels := []marketplaceschema.Channel{
		{ID: "channel-a", ProviderType: "openai_compatible"},
		{ID: "channel-b", ProviderType: "openai_compatible"},
	}
	groups := []marketplaceschema.Group{
		{ID: "group-a", ChannelID: "channel-a", PublicSlug: "group-a", InternalGroupName: "market_group_a", Visibility: marketplacedomain.VisibilityPublic, LifecycleStatus: marketplacedomain.LifecycleActive},
		{ID: "group-b", ChannelID: "channel-b", PublicSlug: "group-b", InternalGroupName: "market_group_b", Visibility: marketplacedomain.VisibilityPublic, LifecycleStatus: marketplacedomain.LifecycleActive},
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&groups).Error)

	requestedChannels := map[string]marketplaceschema.Channel{"channel-a": channels[0]}
	snapshots, err := rankingSnapshotsForRequest(groups[:1], requestedChannels, 24)
	require.NoError(t, err)
	require.Contains(t, snapshots, "group-a")
	require.NotContains(t, snapshots, "group-b")

	var stored []marketplaceschema.RankingSnapshot
	require.NoError(t, db.Order("group_id ASC").Find(&stored).Error)
	require.Len(t, stored, 2)
	require.Equal(t, "group-a", stored[0].GroupID)
	require.Equal(t, "group-b", stored[1].GroupID)
}

func TestPartitionRankingGroupsKeepsPrivateChannelsRequestScoped(t *testing.T) {
	groups := []marketplaceschema.Group{
		{ID: "public", ChannelID: "channel-public", Visibility: marketplacedomain.VisibilityPublic},
		{ID: "private", ChannelID: "channel-private", Visibility: marketplacedomain.VisibilityPrivate},
	}
	channels := map[string]marketplaceschema.Channel{
		"channel-public":  {ID: "channel-public"},
		"channel-private": {ID: "channel-private"},
	}

	publicGroups, publicChannels, privateGroups, privateChannels := partitionRankingGroups(groups, channels)
	require.Equal(t, []marketplaceschema.Group{groups[0]}, publicGroups)
	require.Equal(t, []marketplaceschema.Group{groups[1]}, privateGroups)
	require.Contains(t, publicChannels, "channel-public")
	require.NotContains(t, publicChannels, "channel-private")
	require.Contains(t, privateChannels, "channel-private")
	require.NotContains(t, privateChannels, "channel-public")
}
