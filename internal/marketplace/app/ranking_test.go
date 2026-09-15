package app

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
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
	originalLogDB := platformdb.LogDB
	t.Cleanup(func() { platformdb.LogDB = originalLogDB })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.LogDB = db
	require.NoError(t, db.AutoMigrate(&auditschema.Log{}))
	now := time.Now().Unix()
	require.NoError(t, db.Create([]auditschema.Log{
		{UserId: 1, CreatedAt: now, Type: auditschema.LogTypeConsume, Group: "default", ChannelId: 501, Quota: 100, Other: `{"billing_source":"wallet"}`},
		{UserId: 2, CreatedAt: now, Type: auditschema.LogTypeConsume, Group: "plus", ChannelId: 501, Quota: 300, Other: `{ "billing_source" : "wallet" }`},
		{UserId: 1, CreatedAt: now, Type: auditschema.LogTypeConsume, Group: "pro", ChannelId: 501, Quota: 500, Other: `{"billing_source":"subscription"}`},
		{UserId: 3, CreatedAt: now, Type: auditschema.LogTypeConsume, Group: "default", ChannelId: 502, Quota: 200, Other: `{"billing_source":"subscription"}`},
		{UserId: 4, CreatedAt: now, Type: auditschema.LogTypeConsume, Group: "default", ChannelId: 502, Quota: 700, Other: `{}`},
	}).Error)

	stats := channelConsumerStatsByChannel([]int{501, 502}, 24)
	require.EqualValues(t, 2, stats[501].IndependentConsumers)
	require.EqualValues(t, 2, stats[501].WalletRequestCount)
	require.EqualValues(t, 400, stats[501].WalletConsumerAmount)
	require.EqualValues(t, 200, stats[501].averageConsumerAmount())
	require.EqualValues(t, 2, stats[502].IndependentConsumers)
	require.Zero(t, stats[502].WalletRequestCount)
	require.Zero(t, stats[502].WalletConsumerAmount)
	require.Zero(t, stats[502].averageConsumerAmount())
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

func TestScoreGroupPublishesAverageConsumerAmount(t *testing.T) {
	t.Parallel()

	snapshot := scoreGroup(
		marketplaceschema.Group{ID: "priced", Multiplier: 1},
		rankingTotals{requestCount: 3, successWeight: 3, successTotal: 300},
		channelConsumerStats{IndependentConsumers: 2, WalletRequestCount: 3, WalletConsumerAmount: 901},
		24,
	)

	require.EqualValues(t, 300, snapshot.AvgConsumerAmount)
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

	require.Equal(t, 67.53, snapshot.Score)
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

func TestOfficialGroupMatchesAnySelectedModel(t *testing.T) {
	t.Parallel()

	item := GroupListItem{
		ID:     "official:pro",
		Models: []string{"gpt-6-astra", "claude-sonnet-4-5"},
	}
	require.True(t, matchesGroupListItemQuery(item, GroupQuery{Models: []string{"gemini", "sonnet"}}))
	require.False(t, matchesGroupListItemQuery(item, GroupQuery{Models: []string{"gemini", "deepseek"}}))
}
