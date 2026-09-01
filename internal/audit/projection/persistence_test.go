package projection

import (
	"testing"

	"github.com/glebarez/sqlite"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPerfMetricGroupSummariesQuoteReservedGroupColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&perfMetricRecord{}))

	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })

	require.NoError(t, db.Create([]perfMetricRecord{
		{ModelName: "gpt-test", Group: "default", BucketTs: 10, RequestCount: 3, SuccessCount: 2},
		{ModelName: "gpt-test", Group: "backup", BucketTs: 10, RequestCount: 2, SuccessCount: 2},
		{ModelName: "claude-test", Group: "default", BucketTs: 10, RequestCount: 4, SuccessCount: 4},
	}).Error)

	byGroup, err := getPerfMetricsSummaryByGroups(0, 20, []string{"default", "backup"})
	require.NoError(t, err)
	require.Len(t, byGroup, 2)

	byModel, err := getPerfMetricsSummaryByGroupModels(0, 20, []string{"default"})
	require.NoError(t, err)
	require.Len(t, byModel, 2)
}
