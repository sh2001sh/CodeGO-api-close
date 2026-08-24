package projection

import (
	"context"
	"fmt"
	"sort"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	usageDailyCursorName = "logs"
	usageDailyBatchSize  = 500
)

func rebuildUsageDaily(ctx context.Context) error {
	return rebuildUsageDailyAt(ctx, time.Now().UTC())
}

func rebuildUsageDailyAt(ctx context.Context, now time.Time) error {
	if platformdb.DB == nil || platformdb.LogDB == nil {
		return fmt.Errorf("usage daily databases are not initialized")
	}
	maxLogID, err := usageDailyMaxLogID(ctx)
	if err != nil {
		return err
	}
	cursor, found, err := loadUsageDailyCursor(ctx)
	if err != nil {
		return err
	}
	days, err := affectedUsageDays(ctx, now, cursor.LastLogID, maxLogID, !found)
	if err != nil {
		return err
	}
	userRows, channelRows, err := aggregateUsageDays(ctx, days)
	if err != nil {
		return err
	}
	return replaceUsageDays(ctx, now, maxLogID, days, userRows, channelRows)
}

func usageDailyMaxLogID(ctx context.Context) (int64, error) {
	var result struct {
		MaxID int64 `gorm:"column:max_id"`
	}
	err := platformdb.LogDB.WithContext(ctx).Model(&auditschema.Log{}).
		Select("COALESCE(MAX(id), 0) AS max_id").Scan(&result).Error
	return result.MaxID, err
}

func loadUsageDailyCursor(ctx context.Context) (UsageDailyCursor, bool, error) {
	var cursor UsageDailyCursor
	err := platformdb.DB.WithContext(ctx).Where("name = ?", usageDailyCursorName).First(&cursor).Error
	if err == gorm.ErrRecordNotFound {
		return cursor, false, nil
	}
	return cursor, err == nil, err
}

func affectedUsageDays(ctx context.Context, now time.Time, lastLogID, maxLogID int64, firstRun bool) ([]string, error) {
	days := map[string]struct{}{
		now.UTC().Format(time.DateOnly):                   {},
		now.UTC().AddDate(0, 0, -1).Format(time.DateOnly): {},
	}
	fullBackfill, err := usageDailyNeedsFullBackfill(ctx, firstRun, lastLogID, maxLogID)
	if err != nil {
		return nil, err
	}
	changedDays := []string(nil)
	if !firstRun || fullBackfill {
		changedDays, err = queryChangedUsageDays(ctx, lastLogID, maxLogID, fullBackfill)
	}
	if err != nil {
		return nil, err
	}
	for _, day := range changedDays {
		if day != "" {
			days[day] = struct{}{}
		}
	}
	result := make([]string, 0, len(days))
	for day := range days {
		result = append(result, day)
	}
	sort.Strings(result)
	return result, nil
}

func usageDailyNeedsFullBackfill(ctx context.Context, firstRun bool, lastLogID, maxLogID int64) (bool, error) {
	if !firstRun {
		return maxLogID < lastLogID, nil
	}
	var userCount int64
	if err := platformdb.DB.WithContext(ctx).Model(&UserUsageDaily{}).Count(&userCount).Error; err != nil {
		return false, err
	}
	var channelCount int64
	if err := platformdb.DB.WithContext(ctx).Model(&ChannelUsageDaily{}).Count(&channelCount).Error; err != nil {
		return false, err
	}
	return userCount == 0 || channelCount == 0, nil
}

func queryChangedUsageDays(ctx context.Context, lastLogID, maxLogID int64, fullBackfill bool) ([]string, error) {
	if maxLogID == 0 || (!fullBackfill && maxLogID <= lastLogID) {
		return nil, nil
	}
	day := usageDayExpression(platformdb.LogDB.Dialector.Name())
	query := platformdb.LogDB.WithContext(ctx).Model(&auditschema.Log{}).
		Where("type = ?", auditschema.LogTypeConsume).
		Where("id <= ?", maxLogID)
	if !fullBackfill {
		query = query.Where("id > ?", lastLogID)
	}
	var rows []struct {
		Day string `gorm:"column:day"`
	}
	err := query.Select("DISTINCT " + day + " AS day").Scan(&rows).Error
	days := make([]string, 0, len(rows))
	for _, row := range rows {
		days = append(days, row.Day)
	}
	return days, err
}

func aggregateUsageDays(ctx context.Context, days []string) ([]UserUsageDaily, []ChannelUsageDaily, error) {
	day := usageDayExpression(platformdb.LogDB.Dialector.Name())
	base := platformdb.LogDB.WithContext(ctx).Model(&auditschema.Log{}).
		Where("type = ?", auditschema.LogTypeConsume).
		Where(day+" IN ?", days)
	userRows := []UserUsageDaily{}
	if err := base.Select(usageDailySelect(day, "user_id")).Group(day + ", user_id").Scan(&userRows).Error; err != nil {
		return nil, nil, err
	}
	channelRows := []ChannelUsageDaily{}
	if err := base.Select(usageDailySelect(day, "channel_id")).Group(day + ", channel_id").Scan(&channelRows).Error; err != nil {
		return nil, nil, err
	}
	return userRows, channelRows, nil
}

func usageDailySelect(day, identityColumn string) string {
	return day + " AS day, " + identityColumn +
		", COUNT(*) AS request_count, COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens" +
		", COALESCE(SUM(completion_tokens), 0) AS completion_tokens, COALESCE(SUM(quota), 0) AS quota"
}

func replaceUsageDays(ctx context.Context, now time.Time, maxLogID int64, days []string, users []UserUsageDaily, channels []ChannelUsageDaily) error {
	return platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("day IN ?", days).Delete(&UserUsageDaily{}).Error; err != nil {
			return err
		}
		if err := tx.Where("day IN ?", days).Delete(&ChannelUsageDaily{}).Error; err != nil {
			return err
		}
		if len(users) > 0 {
			if err := tx.CreateInBatches(users, usageDailyBatchSize).Error; err != nil {
				return err
			}
		}
		if len(channels) > 0 {
			if err := tx.CreateInBatches(channels, usageDailyBatchSize).Error; err != nil {
				return err
			}
		}
		cursor := UsageDailyCursor{Name: usageDailyCursorName, LastLogID: maxLogID, UpdatedAt: now.UTC()}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "name"}},
			DoUpdates: clause.AssignmentColumns([]string{"last_log_id", "updated_at"}),
		}).Create(&cursor).Error
	})
}

func usageDayExpression(dialect string) string {
	switch dialect {
	case "postgres":
		return "TO_CHAR(TO_TIMESTAMP(created_at) AT TIME ZONE 'UTC', 'YYYY-MM-DD')"
	case "mysql":
		return "DATE_FORMAT(TIMESTAMPADD(SECOND, created_at, '1970-01-01'), '%Y-%m-%d')"
	default:
		return "date(created_at, 'unixepoch')"
	}
}
