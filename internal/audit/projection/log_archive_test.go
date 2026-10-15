package projection

import (
	"context"
	"errors"
	"testing"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	platformarchive "github.com/sh2001sh/new-api/internal/platform/archivex"
	"github.com/stretchr/testify/require"
)

type capturingArchiveSink struct {
	batch platformarchive.Batch
	err   error
}

func (sink *capturingArchiveSink) Store(_ context.Context, batch platformarchive.Batch) error {
	sink.batch = batch
	return sink.err
}

func TestArchiveOldLogsBatchRequiresProjectedWatermark(t *testing.T) {
	db := setupUsageDailyTestDB(t)
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&[]auditschema.Log{
		{Id: 1, Type: auditschema.LogTypeConsume, CreatedAt: now.AddDate(0, 0, -100).Unix()},
		{Id: 2, Type: auditschema.LogTypeConsume, CreatedAt: now.AddDate(0, 0, -100).Unix()},
	}).Error)
	require.NoError(t, db.Create(&UsageDailyCursor{Name: usageDailyCursorName, LastLogID: 1}).Error)
	sink := &capturingArchiveSink{}

	deleted, err := ArchiveOldLogsBatch(context.Background(), sink, now, 90, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	require.Len(t, sink.batch.Records, 1)
	require.Equal(t, "1-1", sink.batch.ID)
	var remaining []auditschema.Log
	require.NoError(t, db.Order("id asc").Find(&remaining).Error)
	require.Len(t, remaining, 1)
	require.Equal(t, 2, remaining[0].Id)
}

func TestArchiveOldLogsBatchDoesNotDeleteWhenStorageFails(t *testing.T) {
	db := setupUsageDailyTestDB(t)
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&auditschema.Log{Id: 1, Type: auditschema.LogTypeConsume, CreatedAt: now.AddDate(0, 0, -100).Unix()}).Error)
	require.NoError(t, db.Create(&UsageDailyCursor{Name: usageDailyCursorName, LastLogID: 1}).Error)
	sink := &capturingArchiveSink{err: errors.New("storage unavailable")}

	deleted, err := ArchiveOldLogsBatch(context.Background(), sink, now, 90, 100)
	require.ErrorContains(t, err, "storage unavailable")
	require.Zero(t, deleted)
	var count int64
	require.NoError(t, db.Model(&auditschema.Log{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}
