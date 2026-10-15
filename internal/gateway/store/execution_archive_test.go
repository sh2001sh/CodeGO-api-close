package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformarchive "github.com/sh2001sh/new-api/internal/platform/archivex"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type gatewayArchiveSink struct {
	batch platformarchive.Batch
	err   error
}

func (sink *gatewayArchiveSink) Store(_ context.Context, batch platformarchive.Batch) error {
	sink.batch = batch
	return sink.err
}

func TestArchiveSettledExecutionsBatchArchivesCompleteTree(t *testing.T) {
	db := setupExecutionArchiveTestDB(t)
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -100)
	createExecutionArchiveFixture(t, db, "old", "shared-plan", gatewayschema.RequestExecutionStatusSettled, old)
	createExecutionArchiveFixture(t, db, "recent", "shared-plan", gatewayschema.RequestExecutionStatusSettled, now.AddDate(0, 0, -10))
	createExecutionArchiveFixture(t, db, "unfinished", "unfinished-plan", gatewayschema.RequestExecutionStatusProviderComplete, old)
	sink := &gatewayArchiveSink{}

	deleted, err := ArchiveSettledExecutionsBatch(context.Background(), sink, now, 90, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	require.Len(t, sink.batch.Records, 1)
	bundle, ok := sink.batch.Records[0].Data.(gatewayExecutionArchiveBundle)
	require.True(t, ok)
	require.Equal(t, "old", bundle.Execution.ExecutionID)
	require.NotNil(t, bundle.RoutePlan)
	require.Len(t, bundle.Attempts, 1)
	require.Len(t, bundle.UsageEvidence, 1)

	assertExecutionArchiveRowCount(t, db, &gatewayschema.RequestExecution{}, 2)
	assertExecutionArchiveRowCount(t, db, &gatewayschema.ExecutionAttempt{}, 2)
	assertExecutionArchiveRowCount(t, db, &gatewayschema.UsageEvidence{}, 2)
	var sharedPlan gatewayschema.GatewayRoutePlan
	require.NoError(t, db.First(&sharedPlan, "route_plan_id = ?", "shared-plan").Error)
}

func TestArchiveSettledExecutionsBatchDoesNotDeleteWhenStorageFails(t *testing.T) {
	db := setupExecutionArchiveTestDB(t)
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	createExecutionArchiveFixture(t, db, "old", "old-plan", gatewayschema.RequestExecutionStatusSettled, now.AddDate(0, 0, -100))
	sink := &gatewayArchiveSink{err: errors.New("archive disk full")}

	deleted, err := ArchiveSettledExecutionsBatch(context.Background(), sink, now, 90, 100)
	require.ErrorContains(t, err, "archive disk full")
	require.Zero(t, deleted)
	assertExecutionArchiveRowCount(t, db, &gatewayschema.RequestExecution{}, 1)
	assertExecutionArchiveRowCount(t, db, &gatewayschema.ExecutionAttempt{}, 1)
	assertExecutionArchiveRowCount(t, db, &gatewayschema.UsageEvidence{}, 1)
	assertExecutionArchiveRowCount(t, db, &gatewayschema.GatewayRoutePlan{}, 1)
}

func setupExecutionArchiveTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	originalDB := platformdb.DB
	originalSQLite, originalPostgreSQL, originalMySQL := platformdb.UsingSQLite, platformdb.UsingPostgreSQL, platformdb.UsingMySQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite, platformdb.UsingPostgreSQL, platformdb.UsingMySQL = originalSQLite, originalPostgreSQL, originalMySQL
	})
	platformdb.DB = db
	platformdb.UsingSQLite, platformdb.UsingPostgreSQL, platformdb.UsingMySQL = true, false, false
	require.NoError(t, db.AutoMigrate(
		&gatewayschema.RequestExecution{},
		&gatewayschema.GatewayRoutePlan{},
		&gatewayschema.ExecutionAttempt{},
		&gatewayschema.UsageEvidence{},
	))
	return db
}

func createExecutionArchiveFixture(t *testing.T, db *gorm.DB, id, routePlanID, status string, updatedAt time.Time) {
	t.Helper()
	var planCount int64
	require.NoError(t, db.Model(&gatewayschema.GatewayRoutePlan{}).Where("route_plan_id = ?", routePlanID).Count(&planCount).Error)
	if planCount == 0 {
		require.NoError(t, db.Create(&gatewayschema.GatewayRoutePlan{
			RoutePlanID: routePlanID,
			RequestID:   routePlanID,
			Status:      "recorded",
			CreatedAt:   updatedAt,
			UpdatedAt:   updatedAt,
		}).Error)
	}
	require.NoError(t, db.Create(&gatewayschema.RequestExecution{
		ExecutionID:     id,
		RequestID:       id,
		RoutePlanID:     routePlanID,
		Status:          status,
		UsageEvidenceID: id + "-evidence",
		CreatedAt:       updatedAt,
		UpdatedAt:       updatedAt,
	}).Error)
	require.NoError(t, db.Create(&gatewayschema.ExecutionAttempt{
		AttemptID:   id + "-attempt",
		ExecutionID: id,
		AttemptNo:   1,
		Status:      "provider_completed",
		CreatedAt:   updatedAt,
	}).Error)
	require.NoError(t, db.Create(&gatewayschema.UsageEvidence{
		UsageEvidenceID: id + "-evidence",
		ExecutionID:     id,
		RequestID:       id,
		CreatedAt:       updatedAt,
	}).Error)
}

func assertExecutionArchiveRowCount(t *testing.T, db *gorm.DB, model any, expected int64) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(model).Count(&count).Error)
	require.Equal(t, expected, count)
}
