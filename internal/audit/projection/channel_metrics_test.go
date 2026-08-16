package projection

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestChannelMetricsAggregateAcrossGroupsAndIsolateChannels(t *testing.T) {
	originalDB := platformdb.DB
	originalRedisEnabled := platformcache.RedisEnabled
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformcache.RedisEnabled = originalRedisEnabled
		clearMetricBucketsForChannels(910501, 910502)
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	platformcache.RedisEnabled = false
	require.NoError(t, db.AutoMigrate(&channelPerfMetricRecord{}))
	empty, err := QuerySummaryByChannels(24, []int{910503})
	require.NoError(t, err)
	require.Empty(t, empty)

	Record(Sample{Model: "model-a", Group: "default", ChannelID: 910501, LatencyMs: 1000, TtftMs: 300, HasTtft: true, Success: true, OutputTokens: 40, GenerationMs: 2000})
	Record(Sample{Model: "model-b", Group: "plus", ChannelID: 910501, LatencyMs: 2000, TtftMs: 500, HasTtft: true, Success: false})
	Record(Sample{Model: "model-a", Group: "default", ChannelID: 910502, LatencyMs: 500, TtftMs: 100, HasTtft: true, Success: true})

	summaries, err := QuerySummaryByChannels(24, []int{910501, 910502})
	require.NoError(t, err)
	require.Len(t, summaries, 2)
	require.Equal(t, ChannelSummary{ChannelID: 910501, AvgLatencyMs: 1500, AvgTtftMs: 400, SuccessRate: 50, AvgTps: 20, RequestCount: 2}, summaries[0])
	require.Equal(t, ChannelSummary{ChannelID: 910502, AvgLatencyMs: 500, AvgTtftMs: 100, SuccessRate: 100, AvgTps: 0, RequestCount: 1}, summaries[1])
}

func TestChannelMetricPersistenceUsesChannelAndBucketIdentity(t *testing.T) {
	originalDB := platformdb.DB
	originalRedisEnabled := platformcache.RedisEnabled
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformcache.RedisEnabled = originalRedisEnabled
		clearMetricBucketsForChannels(920501, 920502)
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	platformcache.RedisEnabled = false
	require.NoError(t, db.AutoMigrate(&channelPerfMetricRecord{}))
	bucketTs := bucketStart(time.Now().Unix())

	require.NoError(t, upsertChannelMetric(&channelPerfMetricRecord{ChannelID: 920501, BucketTs: bucketTs, RequestCount: 1, SuccessCount: 1, TotalLatencyMs: 100}))
	require.NoError(t, upsertChannelMetric(&channelPerfMetricRecord{ChannelID: 920501, BucketTs: bucketTs, RequestCount: 1, TotalLatencyMs: 300}))
	require.NoError(t, upsertChannelMetric(&channelPerfMetricRecord{ChannelID: 920502, BucketTs: bucketTs, RequestCount: 1, SuccessCount: 1, TotalLatencyMs: 50}))

	summaries, err := QuerySummaryByChannels(24, []int{920501, 920502})
	require.NoError(t, err)
	require.Len(t, summaries, 2)
	require.EqualValues(t, 2, summaries[0].RequestCount)
	require.Equal(t, 50.0, summaries[0].SuccessRate)
	require.EqualValues(t, 200, summaries[0].AvgLatencyMs)
	require.EqualValues(t, 1, summaries[1].RequestCount)
}

func TestChannelMetricsCalculateCacheHitRateAcrossTokenClasses(t *testing.T) {
	originalDB := platformdb.DB
	originalRedisEnabled := platformcache.RedisEnabled
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformcache.RedisEnabled = originalRedisEnabled
		clearMetricBucketsForChannels(930501)
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	platformcache.RedisEnabled = false
	require.NoError(t, db.AutoMigrate(&channelPerfMetricRecord{}))

	Record(Sample{
		Model: "cache-model", Group: "cache-group", ChannelID: 930501,
		Success: true, InputTokens: 600, CacheReadTokens: 300, CacheWriteTokens: 100,
	})

	summaries, err := QuerySummaryByChannels(24, []int{930501})
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, 30.0, summaries[0].CacheHitRate)

	series, err := QuerySeriesByChannels(6, []int{930501})
	require.NoError(t, err)
	require.Len(t, series, 1)
	require.Len(t, series[0].Series, 1)
	require.Equal(t, 30.0, series[0].Series[0].CacheHitRate)
}

func clearMetricBucketsForChannels(channelIDs ...int) {
	allowed := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		allowed[channelID] = struct{}{}
	}
	channelHotBuckets.Range(func(key, _ any) bool {
		bucketKey := key.(channelBucketKey)
		if _, ok := allowed[bucketKey.channelID]; ok {
			channelHotBuckets.Delete(key)
		}
		return true
	})
	hotBuckets.Range(func(key, _ any) bool {
		metricKey := key.(bucketKey)
		if metricKey.model == "model-a" || metricKey.model == "model-b" {
			hotBuckets.Delete(key)
		}
		return true
	})
}
