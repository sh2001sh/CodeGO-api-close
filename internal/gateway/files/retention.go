package files

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	gatewaySchema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var cleanupWorkerOnce sync.Once

// StartCleanupWorker starts the gateway's periodic expired-file cleanup loop.
func StartCleanupWorker() {
	settings := retentionSettingsFromEnv()
	if settings.Retention <= 0 {
		platformobservability.SysLog("gateway file retention disabled")
		return
	}
	cleanupWorkerOnce.Do(func() {
		platformobservability.SysLog(fmt.Sprintf(
			"gateway file cleanup started: retention=%s interval=%s batch=%d",
			settings.Retention, settings.CleanupInterval, settings.BatchSize,
		))
		go runCleanupWorker(settings)
	})
}

func runCleanupWorker(settings retentionSettings) {
	ticker := time.NewTicker(settings.CleanupInterval)
	defer ticker.Stop()
	for {
		runCleanupCycle(time.Now().UTC(), settings)
		<-ticker.C
	}
}

func runCleanupCycle(now time.Time, settings retentionSettings) {
	for cycle := 0; cycle < 4; cycle++ {
		deleted, err := cleanupExpiredFiles(now, settings)
		if err != nil {
			platformobservability.SysError("gateway file cleanup failed: " + err.Error())
		}
		if deleted > 0 {
			platformobservability.SysLog(fmt.Sprintf("gateway file cleanup completed: deleted=%d", deleted))
		}
		if deleted < int64(settings.BatchSize) {
			return
		}
	}
}

// MarkUsed refreshes the sliding retention window for actual file use.
func MarkUsed(file *gatewaySchema.UserFile) error {
	return markUsedAt(file, time.Now().UTC(), retentionSettingsFromEnv().Retention)
}

func markUsedAt(file *gatewaySchema.UserFile, now time.Time, retention time.Duration) error {
	if file == nil || isExpiredAt(file, now, retention) {
		return ErrNotFound
	}
	if file.LastUsedAt != nil && now.Sub(*file.LastUsedAt) < fileUsageTouchInterval {
		return nil
	}
	query := platformdb.DB.Model(&gatewaySchema.UserFile{}).Where("id = ?", file.ID)
	if retention > 0 {
		query = query.Where("COALESCE(last_used_at, created_at) >= ?", now.Add(-retention))
	}
	result := query.Update("last_used_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	file.LastUsedAt = &now
	return nil
}

// ExpiresAt returns the current sliding expiration time, or nil when disabled.
func ExpiresAt(file *gatewaySchema.UserFile) *time.Time {
	retention := retentionSettingsFromEnv().Retention
	if file == nil || retention <= 0 {
		return nil
	}
	expires := effectiveLastUsedAt(file).Add(retention)
	return &expires
}

func isExpiredAt(file *gatewaySchema.UserFile, now time.Time, retention time.Duration) bool {
	return file == nil || retention > 0 && effectiveLastUsedAt(file).Before(now.Add(-retention))
}

func effectiveLastUsedAt(file *gatewaySchema.UserFile) time.Time {
	if file.LastUsedAt != nil && !file.LastUsedAt.IsZero() {
		return file.LastUsedAt.UTC()
	}
	return file.CreatedAt.UTC()
}

func cleanupExpiredFiles(now time.Time, settings retentionSettings) (int64, error) {
	if settings.Retention <= 0 {
		return 0, nil
	}
	cutoff := now.Add(-settings.Retention)
	var candidates []gatewaySchema.UserFile
	err := platformdb.DB.Where("COALESCE(last_used_at, created_at) < ?", cutoff).
		Order("COALESCE(last_used_at, created_at) ASC").Limit(settings.BatchSize).Find(&candidates).Error
	if err != nil {
		return 0, err
	}
	var deleted int64
	var cleanupErrors []error
	for i := range candidates {
		removed, removeErr := deleteExpiredFile(&candidates[i], cutoff)
		if removed {
			deleted++
		}
		if removeErr != nil {
			cleanupErrors = append(cleanupErrors, removeErr)
		}
	}
	return deleted, errors.Join(cleanupErrors...)
}

func deleteExpiredFile(file *gatewaySchema.UserFile, cutoff time.Time) (bool, error) {
	removed := false
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var current gatewaySchema.UserFile
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", file.ID).First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil || !effectiveLastUsedAt(&current).Before(cutoff) {
			return err
		}
		if current.StoragePath != "" {
			if err := os.Remove(contentPath(current.StoragePath)); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove expired file %s: %w", current.ID, err)
			}
		}
		if err := deleteUpstreamMappings(tx, current.ID); err != nil {
			return err
		}
		if err := tx.Delete(&gatewaySchema.UserFile{}, "id = ?", current.ID).Error; err != nil {
			return err
		}
		removed = true
		return nil
	})
	return removed, err
}

func activeFilesQuery(db *gorm.DB, now time.Time) *gorm.DB {
	retention := retentionSettingsFromEnv().Retention
	if retention <= 0 {
		return db
	}
	return db.Where("COALESCE(last_used_at, created_at) >= ?", now.Add(-retention))
}
