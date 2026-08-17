package app

import (
	"testing"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestMultiplierTrendAggregatesReliableListedAndMedian(t *testing.T) {
	start := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	rows := []marketplaceschema.MultiplierTrendSnapshot{
		{GroupID: "reliable", SourceLabel: "Codex Plus", Models: `["gpt-5.6-sol"]`, Multiplier: 0.8, Reliable: true, BucketStartedAt: start},
		{GroupID: "cheap", SourceLabel: "Codex Plus", Models: `["gpt-5.6-sol"]`, Multiplier: 0.5, Reliable: false, BucketStartedAt: start},
	}
	sources := buildMultiplierTrendSources(rows, "gpt-5.6-sol", start, start, multiplierSnapshotInterval)
	require.Len(t, sources, 1)
	require.Len(t, sources[0].Points, 1)
	point := sources[0].Points[0]
	require.InDelta(t, 0.8, *point.ReliableMin, 0.0001)
	require.InDelta(t, 0.5, *point.ListedMin, 0.0001)
	require.InDelta(t, 0.65, *point.Median, 0.0001)
	require.Equal(t, "reliable", point.ReliableGroupID)
	require.Equal(t, 1, point.EligibleCount)
	require.Equal(t, 2, point.TotalCount)
}

func TestMultiplierTrendKeepsReliableGapAndFiltersModel(t *testing.T) {
	start := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	rows := []marketplaceschema.MultiplierTrendSnapshot{
		{GroupID: "sol", SourceLabel: "Codex Plus", Models: `["gpt-5.6-sol"]`, Multiplier: 0.5, Reliable: false, BucketStartedAt: start},
		{GroupID: "terra", SourceLabel: "Codex Pro", Models: `["gpt-5.6-terra"]`, Multiplier: 0.7, Reliable: true, BucketStartedAt: start},
	}
	sources := buildMultiplierTrendSources(rows, "gpt-5.6-sol", start, start.Add(time.Hour), multiplierSnapshotInterval)
	require.Len(t, sources, 1)
	require.Equal(t, "Codex Plus", sources[0].Source)
	require.Len(t, sources[0].Points, 3)
	for _, point := range sources[0].Points {
		require.Nil(t, point.ReliableMin)
		require.NotNil(t, point.ListedMin)
	}
}

func TestMultiplierTrendSnapshotUpsertKeepsOneRowPerBucket(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.MultiplierTrendSnapshot{}))
	bucket := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	row := marketplaceschema.MultiplierTrendSnapshot{
		GroupID: "group", ChannelID: "channel", SourceLabel: "Codex Plus", Models: `[]`,
		Multiplier: 0.8, BucketStartedAt: bucket, CapturedAt: bucket,
	}
	require.NoError(t, upsertMultiplierTrendSnapshot(row))
	row.Multiplier = 0.6
	row.Reliable = true
	require.NoError(t, upsertMultiplierTrendSnapshot(row))
	var count int64
	require.NoError(t, platformdb.DB.Model(&marketplaceschema.MultiplierTrendSnapshot{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
	var stored marketplaceschema.MultiplierTrendSnapshot
	require.NoError(t, platformdb.DB.First(&stored).Error)
	require.InDelta(t, 0.6, stored.Multiplier, 0.0001)
	require.True(t, stored.Reliable)
}
