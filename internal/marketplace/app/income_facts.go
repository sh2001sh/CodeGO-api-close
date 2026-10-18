package app

import (
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"gorm.io/gorm"
)

// incomeFacts uses the transactionally maintained PostgreSQL projection when
// installed. Older databases and other dialects continue to read settlements.
// Installation publishes the table, backfill and triggers in one transaction.
func incomeFacts(db *gorm.DB, start, end int64) (*gorm.DB, error) {
	raw := db.Session(&gorm.Session{NewDB: true}).Model(&marketplaceschema.Settlement{}).
		Select(`owner_user_id, group_id, 1 AS request_count,
			owner_net_amount AS total_income,
			CASE WHEN status = 'pending' THEN owner_net_amount ELSE 0 END AS pending_income,
			CASE WHEN status = 'released' THEN owner_net_amount - reclaimed_amount ELSE 0 END AS released_income,
			CASE WHEN status = 'reclaimed' THEN owner_net_amount ELSE reclaimed_amount END AS reclaimed_income,
			CASE WHEN status = 'forfeited' THEN owner_net_amount ELSE 0 END AS forfeited_income`)
	if start > 0 {
		raw = raw.Where("created_at >= ?", time.Unix(start, 0))
	}
	if end > 0 {
		raw = raw.Where("created_at < ?", time.Unix(end+1, 0))
	}
	projected := false
	if db.Dialector.Name() == "postgres" {
		if err := db.Raw("SELECT to_regclass('marketplace.income_hourly') IS NOT NULL").Scan(&projected).Error; err != nil {
			return nil, err
		}
	}
	if !projected {
		return db.Table("(?) AS income", raw), nil
	}
	hours := db.Session(&gorm.Session{NewDB: true}).Table("marketplace.income_hourly").
		Select("owner_user_id, group_id, request_count, total_income, pending_income, released_income, reclaimed_income, forfeited_income")
	first, last := int64(0), int64(0)
	if start > 0 {
		first = (start + 3599) / 3600 * 3600
		hours = hours.Where("bucket_start >= ?", first)
	}
	if end > 0 {
		last = (end + 1) / 3600 * 3600
		hours = hours.Where("bucket_start < ?", last)
	}
	// Only the two partial edge hours need the exact original timestamps.
	switch {
	case start > 0 && end > 0:
		raw = raw.Where("created_at < ? OR created_at >= ?", time.Unix(first, 0), time.Unix(last, 0))
	case start > 0:
		raw = raw.Where("created_at < ?", time.Unix(first, 0))
	case end > 0:
		raw = raw.Where("created_at >= ?", time.Unix(last, 0))
	default:
		return db.Table("(?) AS income", hours), nil
	}
	return db.Table("(? UNION ALL ?) AS income", hours, raw), nil
}

const incomeSums = `SUM(request_count) AS request_count,
	COALESCE(SUM(total_income), 0) AS total_income,
	COALESCE(SUM(pending_income), 0) AS pending_income,
	COALESCE(SUM(released_income), 0) AS released_income,
	COALESCE(SUM(reclaimed_income), 0) AS reclaimed_income,
	COALESCE(SUM(forfeited_income), 0) AS forfeited_income`
