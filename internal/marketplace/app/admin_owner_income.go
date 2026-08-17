package app

import (
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

// ListAdminOwnerIncome aggregates settlement history independently of current channels.
func ListAdminOwnerIncome(input AdminOwnerIncomeQuery) (*AdminOwnerIncomeResult, error) {
	if input.StartTimestamp > 0 && input.EndTimestamp > 0 && input.StartTimestamp > input.EndTimestamp {
		input.StartTimestamp, input.EndTimestamp = input.EndTimestamp, input.StartTimestamp
	}
	query := platformdb.DB.Model(&marketplaceschema.Settlement{}).
		Select(`owner_user_id,
			COUNT(*) AS request_count,
			COALESCE(SUM(owner_net_amount), 0) AS total_income,
			COALESCE(SUM(CASE WHEN status = 'pending' THEN owner_net_amount ELSE 0 END), 0) AS pending_income,
			COALESCE(SUM(CASE WHEN status = 'released' THEN owner_net_amount ELSE 0 END), 0) AS released_income`)
	if input.StartTimestamp > 0 {
		query = query.Where("created_at >= ?", time.Unix(input.StartTimestamp, 0))
	}
	if input.EndTimestamp > 0 {
		query = query.Where("created_at < ?", time.Unix(input.EndTimestamp+1, 0))
	}

	result := &AdminOwnerIncomeResult{Items: []AdminOwnerIncomeItem{}}
	if err := query.Group("owner_user_id").Order("total_income DESC, owner_user_id ASC").Scan(&result.Items).Error; err != nil {
		return nil, err
	}
	result.OwnerCount = len(result.Items)
	for _, item := range result.Items {
		result.RequestCount += item.RequestCount
		result.TotalIncome += item.TotalIncome
		result.PendingIncome += item.PendingIncome
		result.ReleasedIncome += item.ReleasedIncome
	}
	return result, nil
}
