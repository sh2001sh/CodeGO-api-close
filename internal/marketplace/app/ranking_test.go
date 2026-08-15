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

func TestIndependentConsumerCountsByChannelAcrossGroups(t *testing.T) {
	originalLogDB := platformdb.LogDB
	t.Cleanup(func() { platformdb.LogDB = originalLogDB })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.LogDB = db
	require.NoError(t, db.AutoMigrate(&auditschema.Log{}))
	now := time.Now().Unix()
	require.NoError(t, db.Create([]auditschema.Log{
		{UserId: 1, CreatedAt: now, Type: auditschema.LogTypeConsume, Group: "default", ChannelId: 501},
		{UserId: 2, CreatedAt: now, Type: auditschema.LogTypeConsume, Group: "plus", ChannelId: 501},
		{UserId: 1, CreatedAt: now, Type: auditschema.LogTypeConsume, Group: "pro", ChannelId: 501},
		{UserId: 3, CreatedAt: now, Type: auditschema.LogTypeConsume, Group: "default", ChannelId: 502},
	}).Error)

	counts := independentConsumerCountsByChannel([]int{501, 502}, 24)
	require.EqualValues(t, 2, counts[501])
	require.EqualValues(t, 1, counts[502])
}

func TestAggregateChannelRankingRowsKeepsChannelsSeparate(t *testing.T) {
	t.Parallel()

	totals := aggregateChannelRankingRows([]auditprojection.ChannelSummary{
		{ChannelID: 501, SuccessRate: 75, AvgLatencyMs: 1200, AvgTtftMs: 300, AvgTps: 40, RequestCount: 20},
		{ChannelID: 502, SuccessRate: 100, AvgLatencyMs: 500, AvgTtftMs: 100, AvgTps: 80, RequestCount: 2},
	})

	require.EqualValues(t, 20, totals[501].requestCount)
	require.Equal(t, 1500.0, totals[501].successTotal)
	require.EqualValues(t, 2, totals[502].requestCount)
	require.Equal(t, 200.0, totals[502].successTotal)
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
