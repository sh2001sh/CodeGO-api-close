package projection

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRankingMetricsIncludeOfficialAndThirdPartyExactlyOnce(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&perfMetricRecord{}, &channelPerfMetricRecord{}))
	originalDB, originalRedis := platformdb.DB, platformcache.RedisEnabled
	platformdb.DB, platformcache.RedisEnabled = db, false
	t.Cleanup(func() { platformdb.DB, platformcache.RedisEnabled = originalDB, originalRedis })
	now := bucketStart(time.Now().Unix())
	require.NoError(t, db.Create([]perfMetricRecord{
		{ModelName: "shared", Group: "official", BucketTs: now, RequestCount: 3, SuccessCount: 2, InputTokens: 300, TotalLatencyMs: 3000},
		{ModelName: "shared", Group: "third-party", BucketTs: now, RequestCount: 5, SuccessCount: 5, InputTokens: 1000, TotalLatencyMs: 10000},
		{ModelName: "third-only", Group: "third-party", BucketTs: now, RequestCount: 2, SuccessCount: 2, InputTokens: 80},
		{ModelName: "shared", Group: "private", BucketTs: now, RequestCount: 1000, InputTokens: 99999},
		{ModelName: "secret", Group: "official", BucketTs: now, RequestCount: 999},
		{ModelName: "expired", Group: "official", BucketTs: now - 31*24*3600, RequestCount: 999},
	}).Error)
	// A channel aggregate represents the same requests and must never be added
	// to the model projection a second time.
	require.NoError(t, db.Create(&channelPerfMetricRecord{ChannelID: 12, BucketTs: now, RequestCount: 10000}).Error)
	for _, sample := range []Sample{
		{Model: "shared", Group: "third-party", Success: true, InputTokens: 40, LatencyMs: 900},
		{Model: "secret", Group: "third-party", InputTokens: 9999},
		{Model: "shared", Group: "private", InputTokens: 9999},
	} {
		key := bucketKey{model: sample.Model, group: sample.Group, bucketTs: now}
		t.Cleanup(func() { hotBuckets.Delete(key) })
		Record(sample)
	}
	scope := map[string][]string{"official": {"shared", "expired"}, "third-party": {"shared", "shared", "third-only"}}
	summary, err := QueryRankingSummary(24, scope)
	require.NoError(t, err)
	require.Len(t, summary.Models, 2)
	require.Equal(t, "shared", summary.Models[0].ModelName)
	require.EqualValues(t, 9, summary.Models[0].RequestCount)
	require.EqualValues(t, 1340, summary.Models[0].InputTokens)
	require.EqualValues(t, 1544, summary.Models[0].AvgLatencyMs)
	require.InDelta(t, 88.89, summary.Models[0].SuccessRate, .01)
	require.Equal(t, "third-only", summary.Models[1].ModelName)
	trend, err := QueryRankingTrend("shared", 24, scope)
	require.NoError(t, err)
	require.Len(t, trend.Groups, 2)
	var requests, inputs int64
	for _, group := range trend.Groups {
		for _, point := range group.Series {
			requests += point.RequestCount
			inputs += point.InputTokens
		}
	}
	require.EqualValues(t, 9, requests, "trend and model total must cover identical sources")
	require.EqualValues(t, 1340, inputs)
	hidden, err := QueryRankingTrend("secret", 24, scope)
	require.NoError(t, err)
	require.Empty(t, hidden.Groups)
}

func TestRankingMetricsEmptyScopeNeverReadsAllGroups(t *testing.T) {
	originalDB := platformdb.DB
	platformdb.DB = nil
	t.Cleanup(func() { platformdb.DB = originalDB })
	for _, scope := range []map[string][]string{nil, {}, {"official": {}}} {
		summary, err := QueryRankingSummary(24, scope)
		require.NoError(t, err)
		require.NotNil(t, summary.Models)
		require.Empty(t, summary.Models)
		trend, err := QueryRankingTrend("private", 24, scope)
		require.NoError(t, err)
		require.Empty(t, trend.Groups)
	}
}

func TestRankingMetricsKeepPeriodsAndPropagateQueryFailures(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&perfMetricRecord{}))
	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })
	now := time.Now().Unix()
	require.NoError(t, db.Create([]perfMetricRecord{
		{ModelName: "window", Group: "third-party", BucketTs: now - 3600, RequestCount: 1},
		{ModelName: "window", Group: "third-party", BucketTs: now - 48*3600, RequestCount: 2},
		{ModelName: "window", Group: "third-party", BucketTs: now - 10*24*3600, RequestCount: 4},
		{ModelName: "window", Group: "third-party", BucketTs: now - 31*24*3600, RequestCount: 8},
	}).Error)
	scope := map[string][]string{"third-party": {"window"}}
	for _, test := range []struct{ hours, requests int }{{24, 1}, {7 * 24, 3}, {30 * 24, 7}, {90 * 24, 7}} {
		summary, err := QueryRankingSummary(test.hours, scope)
		require.NoError(t, err)
		require.Len(t, summary.Models, 1)
		require.EqualValues(t, test.requests, summary.Models[0].RequestCount)
		trend, err := QueryRankingTrend("window", test.hours, scope)
		require.NoError(t, err)
		var requests int64
		for _, point := range trend.Groups[0].Series {
			requests += point.RequestCount
		}
		require.EqualValues(t, test.requests, requests)
	}
	require.NoError(t, db.Migrator().DropTable(&perfMetricRecord{}))
	_, err = QueryRankingSummary(24, scope)
	require.Error(t, err)
	_, err = QueryRankingTrend("window", 24, scope)
	require.Error(t, err)
}
