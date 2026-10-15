package projection

import (
	"testing"

	"github.com/glebarez/sqlite"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestChannelConsumerMetricsStayExactAcrossFlush(t *testing.T) {
	const channelID = 970501
	originalDB := platformdb.DB
	t.Cleanup(func() {
		platformdb.DB = originalDB
		clearChannelConsumerMetricHot(channelID)
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	require.NoError(t, db.AutoMigrate(&channelConsumerMetricRecord{}, &channelConsumerIdentityRecord{}))

	RecordChannelConsumerMetric(channelID, 1, "market-a", "model-a", 100, 1000, "wallet")
	RecordChannelConsumerMetric(channelID, 2, "market-a", "model-a", 200, 2000, "wallet")
	RecordChannelConsumerMetric(channelID, 3, "market-a", "model-a", 900, 9000, "subscription")

	assertChannelConsumerMetricSummary(t, channelID, 2, 300, 3000, 3)
	flushCompletedChannelConsumerMetrics(0)
	assertChannelConsumerMetricSummary(t, channelID, 2, 300, 3000, 3)

	RecordChannelConsumerMetric(channelID, 4, "market-a", "model-a", 50, 500, "wallet")
	assertChannelConsumerMetricSummary(t, channelID, 3, 350, 3500, 4)

	groups, err := QueryGroupConsumerMetrics(24, []string{"market-a"})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, "model-a", groups[0].ModelName)
	require.EqualValues(t, 3, groups[0].RequestCount)
	require.EqualValues(t, 350, groups[0].Quota)
}

func assertChannelConsumerMetricSummary(t *testing.T, channelID int, requests, quota, tokens, consumers int64) {
	t.Helper()
	rows, err := QueryChannelConsumerMetrics(24, []int{channelID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "model-a", rows[0].ModelName)
	require.Equal(t, requests, rows[0].RequestCount)
	require.Equal(t, quota, rows[0].Quota)
	require.Equal(t, tokens, rows[0].TokenCount)
	identities, err := QueryChannelIndependentConsumers(24, []int{channelID})
	require.NoError(t, err)
	require.Equal(t, consumers, identities[channelID])
}

func clearChannelConsumerMetricHot(channelIDs ...int) {
	allowed := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		allowed[channelID] = struct{}{}
	}
	channelConsumerMetricHot.Range(func(rawKey, _ any) bool {
		if _, ok := allowed[rawKey.(channelConsumerMetricKey).channelID]; ok {
			channelConsumerMetricHot.Delete(rawKey)
		}
		return true
	})
	channelConsumerIdentityHot.Range(func(rawKey, _ any) bool {
		if _, ok := allowed[rawKey.(channelConsumerIdentityRecord).ChannelID]; ok {
			channelConsumerIdentityHot.Delete(rawKey)
		}
		return true
	})
}
