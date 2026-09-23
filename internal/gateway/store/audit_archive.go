package store

import (
	"context"
	"fmt"
	"time"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformarchive "github.com/sh2001sh/new-api/internal/platform/archivex"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

// ArchiveRequestAuditsBatch archives terminal request audit rows before deleting
// them from the hot table. The archive is written first, so a storage failure
// never causes data loss.
func ArchiveRequestAuditsBatch(ctx context.Context, sink platformarchive.Sink, now time.Time, retentionDays, limit int) (int64, error) {
	if sink == nil || platformdb.DB == nil || retentionDays <= 0 || limit <= 0 {
		return 0, nil
	}
	cutoff := now.UTC().AddDate(0, 0, -retentionDays)
	rows := make([]gatewayschema.RequestAudit, 0, limit)
	if err := platformdb.DB.WithContext(ctx).Where("status IN ? AND completed_at < ?", []string{
		gatewayschema.RequestAuditStatusSucceeded, gatewayschema.RequestAuditStatusFailed,
		gatewayschema.RequestAuditStatusRejected, gatewayschema.RequestAuditStatusCancelled,
	}, cutoff).Order("completed_at asc, request_id asc").Limit(limit).Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	records := make([]platformarchive.Record, len(rows))
	ids := make([]string, len(rows))
	for i := range rows {
		records[i] = platformarchive.Record{Type: "request_audit", Data: rows[i]}
		ids[i] = rows[i].RequestID
	}
	batch := platformarchive.Batch{Dataset: "request-audits", Partition: rows[0].CompletedAt.UTC(), ID: fmt.Sprintf("%d-%s", rows[0].CompletedAt.Unix(), ids[len(ids)-1]), Records: records}
	if err := sink.Store(ctx, batch); err != nil {
		return 0, fmt.Errorf("store request audit archive batch: %w", err)
	}
	var deleted int64
	err := platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("request_id IN ? AND status IN ? AND completed_at < ?", ids, []string{
			gatewayschema.RequestAuditStatusSucceeded, gatewayschema.RequestAuditStatusFailed,
			gatewayschema.RequestAuditStatusRejected, gatewayschema.RequestAuditStatusCancelled,
		}, cutoff).Delete(&gatewayschema.RequestAudit{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(len(ids)) {
			return fmt.Errorf("request audit archive delete mismatch: deleted %d of %d", result.RowsAffected, len(ids))
		}
		deleted = result.RowsAffected
		return nil
	})
	return deleted, err
}

// ArchiveRequestAttemptAuditsBatch archives completed upstream attempt audits.
func ArchiveRequestAttemptAuditsBatch(ctx context.Context, sink platformarchive.Sink, now time.Time, retentionDays, limit int) (int64, error) {
	if sink == nil || platformdb.DB == nil || retentionDays <= 0 || limit <= 0 {
		return 0, nil
	}
	cutoff := now.UTC().AddDate(0, 0, -retentionDays)
	rows := make([]gatewayschema.RequestAttemptAudit, 0, limit)
	if err := platformdb.DB.WithContext(ctx).Where("completed_at > ? AND completed_at < ?", time.Unix(0, 0), cutoff).Order("completed_at asc, attempt_id asc").Limit(limit).Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	records := make([]platformarchive.Record, len(rows))
	ids := make([]string, len(rows))
	for i := range rows {
		records[i] = platformarchive.Record{Type: "request_attempt_audit", Data: rows[i]}
		ids[i] = rows[i].AttemptID
	}
	batch := platformarchive.Batch{Dataset: "request-attempt-audits", Partition: rows[0].CompletedAt.UTC(), ID: fmt.Sprintf("%d-%s", rows[0].CompletedAt.Unix(), ids[len(ids)-1]), Records: records}
	if err := sink.Store(ctx, batch); err != nil {
		return 0, fmt.Errorf("store request attempt audit archive batch: %w", err)
	}
	var deleted int64
	err := platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("attempt_id IN ? AND completed_at > ? AND completed_at < ?", ids, time.Unix(0, 0), cutoff).Delete(&gatewayschema.RequestAttemptAudit{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(len(ids)) {
			return fmt.Errorf("request attempt audit archive delete mismatch: deleted %d of %d", result.RowsAffected, len(ids))
		}
		deleted = result.RowsAffected
		return nil
	})
	return deleted, err
}
