package app

import (
	"testing"

	"github.com/glebarez/sqlite"
	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestWilsonLowerBoundPenalizesSmallSamples(t *testing.T) {
	t.Parallel()

	small := wilsonLowerBound(9, 10, 1.96)
	large := wilsonLowerBound(90, 100, 1.96)

	require.Less(t, small, 0.9)
	require.Greater(t, large, small)
	require.Zero(t, wilsonLowerBound(0, 0, 1.96))
}

func TestRankingVersionFitsPersistedColumn(t *testing.T) {
	t.Parallel()
	require.LessOrEqual(t, len(rankingVersion), 32)
}

func TestChannelConsumerStatsByChannelAcrossGroups(t *testing.T) {
	originalDB, originalLogDB, originalMaster := platformdb.DB, platformdb.LogDB, platformconfig.IsMasterNode
	t.Cleanup(func() {
		platformdb.DB, platformdb.LogDB, platformconfig.IsMasterNode = originalDB, originalLogDB, originalMaster
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB, platformdb.LogDB = db, db
	platformconfig.IsMasterNode = true
	require.NoError(t, auditprojection.EnsureSchema())
	auditprojection.RecordChannelConsumerMetric(501, 1, "default", "model-a", 100, 1000, "wallet")
	auditprojection.RecordChannelConsumerMetric(501, 2, "plus", "model-b", 300, 2000, "wallet")
	auditprojection.RecordChannelConsumerMetric(501, 1, "pro", "model-a", 500, 1000, "subscription")
	auditprojection.RecordChannelConsumerMetric(502, 3, "default", "model-a", 200, 1000, "subscription")
	auditprojection.RecordChannelConsumerMetric(502, 4, "default", "model-b", 700, 1000, "")

	stats := channelConsumerStatsByChannel([]int{501, 502}, 24)
	require.EqualValues(t, 2, stats[501].IndependentConsumers)
	require.EqualValues(t, 2, stats[501].WalletRequestCount)
	require.EqualValues(t, 400, stats[501].WalletConsumerAmount)
	require.EqualValues(t, 133333, stats[501].averageConsumerAmount())
	require.Equal(t, map[string]int64{"model-a": 100000, "model-b": 150000}, stats[501].averageConsumerAmountsByModel())
	require.EqualValues(t, 2, stats[502].IndependentConsumers)
	require.Zero(t, stats[502].WalletRequestCount)
	require.Zero(t, stats[502].WalletConsumerAmount)
	require.Zero(t, stats[502].averageConsumerAmount())
	require.Empty(t, stats[502].averageConsumerAmountsByModel())
}

func TestOfficialWalletConsumerStatsSeparatesModelsAndExcludesSubscriptions(t *testing.T) {
	originalDB, originalLogDB, originalMaster := platformdb.DB, platformdb.LogDB, platformconfig.IsMasterNode
	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB, platformdb.LogDB, platformconfig.IsMasterNode = originalDB, originalLogDB, originalMaster
		platformdb.UsingPostgreSQL = originalPostgreSQL
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB, platformdb.LogDB, platformconfig.IsMasterNode = db, db, true
	platformdb.UsingPostgreSQL = false
	require.NoError(t, auditprojection.EnsureSchema())
	auditprojection.RecordChannelConsumerMetric(1, 1, "official-a", "model-a", 100, 1000, "wallet")
	auditprojection.RecordChannelConsumerMetric(1, 2, "official-a", "model-a", 300, 3000, "wallet")
	auditprojection.RecordChannelConsumerMetric(1, 3, "official-a", "model-b", 900, 3000, "wallet")
	auditprojection.RecordChannelConsumerMetric(1, 4, "official-a", "model-a", 5000, 1000, "subscription")
	auditprojection.RecordChannelConsumerMetric(2, 5, "official-b", "model-a", 700, 1400, "wallet")
	officialWalletStatsCache.Lock()
	officialWalletStatsCache.entries = nil
	officialWalletStatsCache.Unlock()

	stats, err := officialWalletConsumerStats([]string{"official-a", "official-b"}, 24)
	require.NoError(t, err)
	require.EqualValues(t, 185714, stats["official-a"].averageConsumerAmount())
	require.Equal(t, map[string]int64{"model-a": 100000, "model-b": 300000}, stats["official-a"].averageConsumerAmountsByModel())
	require.EqualValues(t, 500000, stats["official-b"].averageConsumerAmount())
}

func TestAggregateChannelRankingRowsKeepsChannelsSeparate(t *testing.T) {
	t.Parallel()

	totals := aggregateChannelRankingRows([]auditprojection.ChannelSummary{
		{ChannelID: 501, SuccessRate: 75, AvgLatencyMs: 1200, AttemptTtftP50Ms: 300, AttemptTtftP95Ms: 2000, E2eTtftP50Ms: 500, E2eTtftP95Ms: 3000, AttemptTtftCount: 18, E2eTtftCount: 20, AvgTps: 40, RequestCount: 20},
		{ChannelID: 502, SuccessRate: 100, AvgLatencyMs: 500, AttemptTtftP50Ms: 100, AttemptTtftP95Ms: 250, E2eTtftP50Ms: 250, E2eTtftP95Ms: 500, AttemptTtftCount: 2, E2eTtftCount: 2, AvgTps: 80, RequestCount: 2},
	})

	require.EqualValues(t, 20, totals[501].requestCount)
	require.Equal(t, 1500.0, totals[501].successTotal)
	require.Equal(t, 300.0, totals[501].attemptTtftP50)
	require.EqualValues(t, 18, totals[501].latencySamples)
	require.EqualValues(t, 2, totals[502].requestCount)
	require.Equal(t, 200.0, totals[502].successTotal)
}

func TestScoreGroupDoesNotPromoteLegacyTTFTToPercentile(t *testing.T) {
	t.Parallel()

	snapshot := scoreGroup(marketplaceschema.Group{ID: "fresh", Multiplier: 1}, rankingTotals{
		requestCount: 10, successWeight: 10, successTotal: 1000,
	}, channelConsumerStats{IndependentConsumers: 2}, 24)

	require.Zero(t, snapshot.AvgTTFTMs)
	require.Zero(t, snapshot.AttemptTTFTP50Ms)
	require.Zero(t, snapshot.LatencySampleCount)
}

func TestScoreGroupPublishesConsumerAmountPerMillionTokens(t *testing.T) {
	t.Parallel()

	snapshot := scoreGroup(
		marketplaceschema.Group{ID: "priced", Multiplier: 1},
		rankingTotals{requestCount: 3, successWeight: 3, successTotal: 300},
		channelConsumerStats{IndependentConsumers: 2, WalletRequestCount: 3, WalletConsumerAmount: 901, WalletTokenCount: 3000},
		24,
	)

	require.EqualValues(t, 300333, snapshot.AvgConsumerAmount)
	require.Equal(t, map[string]int64{}, decodeConsumerAmountsByModel(snapshot.AvgConsumerAmountByModel))
}

func TestScoreGroupPreservesCalculatedScorePrecision(t *testing.T) {
	t.Parallel()

	snapshot := scoreGroup(marketplaceschema.Group{ID: "precise", Multiplier: 1.5}, rankingTotals{
		requestCount:   10000,
		successWeight:  10000,
		successTotal:   1000000,
		latencyWeight:  10000,
		latencyTotal:   150000000,
		attemptTtftP50: 1500,
		latencySamples: 10000,
		tpsWeight:      10000,
		tpsTotal:       504000,
		cacheHitRate:   50,
	}, channelConsumerStats{IndependentConsumers: 10}, 24)

	require.Equal(t, 57.53, snapshot.Score)
}

func TestAssignRanksUsesStableTieBreaker(t *testing.T) {
	t.Parallel()

	snapshots := []marketplaceschema.RankingSnapshot{
		{GroupID: "group-b", Score: 90},
		{GroupID: "observing", Score: 100, Observing: true},
		{GroupID: "group-a", Score: 90},
	}

	assignRanks(snapshots)

	require.Equal(t, "group-a", snapshots[0].GroupID)
	require.Equal(t, 1, snapshots[0].Rank)
	require.Equal(t, "group-b", snapshots[1].GroupID)
	require.Equal(t, 2, snapshots[1].Rank)
	require.Equal(t, 0, snapshots[2].Rank)
}

func TestSortGroupItemsKeepsEqualValuesDeterministic(t *testing.T) {
	t.Parallel()

	items := []GroupListItem{{ID: "b", Score: 10}, {ID: "a", Score: 10}}
	sortGroupItems(items, "score", "desc")
	require.Equal(t, []string{"a", "b"}, []string{items[0].ID, items[1].ID})
}

func TestSortGroupItemsPutsMissingTTFTSamplesLast(t *testing.T) {
	t.Parallel()

	for _, direction := range []string{"asc", "desc"} {
		items := []GroupListItem{
			{ID: "missing"},
			{ID: "measured", AttemptTTFTP50Ms: 1000, LatencySampleCount: 10},
		}
		sortGroupItems(items, "ttft", direction)
		require.Equal(t, "measured", items[0].ID)
		require.Equal(t, "missing", items[1].ID)
	}
}

func TestSortGroupItemsUsesSelectedModelConsumerAmountAndPutsMissingLast(t *testing.T) {
	t.Parallel()

	for _, direction := range []string{"asc", "desc"} {
		items := []GroupListItem{
			{ID: "missing", AvgConsumerAmount: 1, AvgConsumerAmountByModel: map[string]int64{}},
			{ID: "cheap", AvgConsumerAmountByModel: map[string]int64{"gpt-x": 100}},
			{ID: "expensive", AvgConsumerAmountByModel: map[string]int64{"gpt-x": 300}},
		}
		sortGroupItems(items, "model_consumer_amount", direction, "GPT-X")
		require.Equal(t, "missing", items[2].ID)
		if direction == "asc" {
			require.Equal(t, []string{"cheap", "expensive", "missing"}, []string{items[0].ID, items[1].ID, items[2].ID})
		} else {
			require.Equal(t, []string{"expensive", "cheap", "missing"}, []string{items[0].ID, items[1].ID, items[2].ID})
		}
	}
}

func TestOfficialGroupMatchesAnySelectedModel(t *testing.T) {
	t.Parallel()

	item := GroupListItem{
		ID:     "official:pro",
		Models: []string{"gpt-6-astra", "claude-sonnet-4-5"},
	}
	require.True(t, matchesGroupListItemQuery(item, GroupQuery{Models: []string{"gemini", "sonnet"}}))
	require.False(t, matchesGroupListItemQuery(item, GroupQuery{Models: []string{"gemini", "deepseek"}}))
}
