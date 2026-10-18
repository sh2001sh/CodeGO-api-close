package settlement

import (
	"context"

	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

// CompactIncomeProjection bounds the append-only projection tail. Queries also
// read the unmerged tail, so a paused worker cannot make the displayed sums stale.
func CompactIncomeProjection(ctx context.Context) (int64, error) {
	db := platformdb.DB.WithContext(ctx)
	if db.Dialector.Name() != "postgres" {
		return 0, nil
	}
	var installed bool
	if err := db.Raw("SELECT to_regclass('marketplace.income_hourly') IS NOT NULL").Scan(&installed).Error; err != nil {
		return 0, err
	}
	if !installed {
		return 0, nil
	}
	var count int64
	err := db.Raw("SELECT marketplace.compact_income_hourly(10000)").Scan(&count).Error
	return count, err
}
