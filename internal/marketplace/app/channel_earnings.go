package app

import (
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

type ownerChannelEarnings struct {
	GroupID         string `gorm:"column:group_id"`
	RequestCount    int64  `gorm:"column:request_count"`
	TotalIncome     int64  `gorm:"column:total_income"`
	PendingIncome   int64  `gorm:"column:pending_income"`
	ReleasedIncome  int64  `gorm:"column:released_income"`
	ReclaimedIncome int64  `gorm:"column:reclaimed_income"`
	ForfeitedIncome int64  `gorm:"column:forfeited_income"`
}

func earningsByGroupIDs(ids []string) (map[string]ownerChannelEarnings, error) {
	return earningsByGroupIDsInRange(ids, 0, 0)
}

func earningsByGroupIDsInRange(ids []string, startTimestamp, endTimestamp int64) (map[string]ownerChannelEarnings, error) {
	result := make(map[string]ownerChannelEarnings, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var rows []ownerChannelEarnings
	query := platformdb.DB.Model(&marketplaceschema.Settlement{}).
		Select(`group_id,
			COUNT(*) AS request_count,
			COALESCE(SUM(owner_net_amount), 0) AS total_income,
			COALESCE(SUM(CASE WHEN status = 'pending' THEN owner_net_amount ELSE 0 END), 0) AS pending_income,
			COALESCE(SUM(CASE WHEN status = 'released' THEN owner_net_amount ELSE 0 END), 0) AS released_income,
			COALESCE(SUM(CASE WHEN status = 'reclaimed' THEN owner_net_amount ELSE 0 END), 0) AS reclaimed_income,
			COALESCE(SUM(CASE WHEN status = 'forfeited' THEN owner_net_amount ELSE 0 END), 0) AS forfeited_income`).
		Where("group_id IN ?", ids)
	if startTimestamp > 0 {
		query = query.Where("created_at >= ?", time.Unix(startTimestamp, 0))
	}
	if endTimestamp > 0 {
		query = query.Where("created_at < ?", time.Unix(endTimestamp+1, 0))
	}
	if err := query.Group("group_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.GroupID] = row
	}
	return result, nil
}
