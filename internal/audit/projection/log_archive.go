package projection

import (
	"context"
	"fmt"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	platformarchive "github.com/sh2001sh/new-api/internal/platform/archivex"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

// ArchiveOldLogsBatch durably stores one eligible batch before deleting the
// exact archived primary keys. A usage cursor is required so rollups cannot
// lose rows that have not yet been projected.
func ArchiveOldLogsBatch(ctx context.Context, sink platformarchive.Sink, now time.Time, retentionDays, limit int) (int64, error) {
	if sink == nil || platformdb.DB == nil || platformdb.LogDB == nil || retentionDays <= 0 || limit <= 0 {
		return 0, nil
	}
	if !platformdb.DB.Migrator().HasTable(&UsageDailyCursor{}) {
		return 0, nil
	}
	cursor, found, err := loadUsageDailyCursor(ctx)
	if err != nil || !found || cursor.LastLogID <= 0 {
		return 0, err
	}
	cutoff := now.UTC().AddDate(0, 0, -retentionDays).Unix()
	logs := make([]auditschema.Log, 0, limit)
	if err := platformdb.LogDB.WithContext(ctx).
		Where("created_at < ? AND id <= ?", cutoff, cursor.LastLogID).
		Order("created_at asc, id asc").
		Limit(limit).
		Find(&logs).Error; err != nil {
		return 0, err
	}
	if len(logs) == 0 {
		return 0, nil
	}
	records := make([]platformarchive.Record, 0, len(logs))
	ids := make([]int, 0, len(logs))
	for index := range logs {
		records = append(records, platformarchive.Record{Type: "log", Data: logs[index]})
		ids = append(ids, logs[index].Id)
	}
	batch := platformarchive.Batch{
		Dataset:   "logs",
		Partition: time.Unix(logs[0].CreatedAt, 0).UTC(),
		ID:        fmt.Sprintf("%d-%d", logs[0].Id, logs[len(logs)-1].Id),
		Records:   records,
	}
	if err := sink.Store(ctx, batch); err != nil {
		return 0, fmt.Errorf("store log archive batch: %w", err)
	}
	var deleted int64
	err = platformdb.LogDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id IN ? AND created_at < ? AND id <= ?", ids, cutoff, cursor.LastLogID).
			Delete(&auditschema.Log{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(len(ids)) {
			return fmt.Errorf("log archive delete mismatch: deleted %d of %d rows", result.RowsAffected, len(ids))
		}
		deleted = result.RowsAffected
		return nil
	})
	return deleted, err
}
