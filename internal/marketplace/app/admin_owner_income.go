package app

import (
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
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
			COALESCE(SUM(CASE WHEN status = 'released' THEN owner_net_amount ELSE 0 END), 0) AS released_income,
			COALESCE(SUM(CASE WHEN status = 'reclaimed' THEN owner_net_amount ELSE 0 END), 0) AS reclaimed_income,
			COALESCE(SUM(CASE WHEN status = 'forfeited' THEN owner_net_amount ELSE 0 END), 0) AS forfeited_income`)
	if normalizedSearch := normalizeExternalIDSearch(input.OwnerSearch); normalizedSearch != "" {
		ownerUserIDs, err := ownerUserIDsByExternalID(normalizedSearch)
		if err != nil {
			return nil, err
		}
		if len(ownerUserIDs) == 0 {
			return &AdminOwnerIncomeResult{Items: []AdminOwnerIncomeItem{}}, nil
		}
		query = query.Where("owner_user_id IN ?", ownerUserIDs)
	}
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
	ownerUserIDs := make([]int, 0, len(result.Items))
	for _, item := range result.Items {
		ownerUserIDs = append(ownerUserIDs, item.OwnerUserID)
	}
	externalIDs, err := ownerExternalIDs(ownerUserIDs)
	if err != nil {
		return nil, err
	}
	result.OwnerCount = len(result.Items)
	for index, item := range result.Items {
		result.Items[index].OwnerExternalID = externalIDs[item.OwnerUserID]
		result.RequestCount += item.RequestCount
		result.TotalIncome += item.TotalIncome
		result.PendingIncome += item.PendingIncome
		result.ReleasedIncome += item.ReleasedIncome
		result.ReclaimedIncome += item.ReclaimedIncome
		result.ForfeitedIncome += item.ForfeitedIncome
	}
	return result, nil
}

func ReleaseAdminOwnerIncome(input AdminOwnerIncomeQuery) (*AdminOwnerIncomeReleaseResult, error) {
	if input.StartTimestamp > 0 && input.EndTimestamp > 0 && input.StartTimestamp > input.EndTimestamp {
		input.StartTimestamp, input.EndTimestamp = input.EndTimestamp, input.StartTimestamp
	}
	ownerIDs := []int(nil)
	if normalizedSearch := normalizeExternalIDSearch(input.OwnerSearch); normalizedSearch != "" {
		var err error
		ownerIDs, err = ownerUserIDsByExternalID(normalizedSearch)
		if err != nil {
			return nil, err
		}
		if len(ownerIDs) == 0 {
			return &AdminOwnerIncomeReleaseResult{}, nil
		}
	}
	if len(input.OwnerUserIDs) > 0 {
		ownerIDs = input.OwnerUserIDs
	}
	result, err := marketplacesettlement.ReclaimPending(marketplacesettlement.ReleaseFilter{
		OwnerUserIDs: ownerIDs, StartTimestamp: input.StartTimestamp,
		EndTimestamp: input.EndTimestamp,
	})
	if err != nil {
		return nil, err
	}
	return &AdminOwnerIncomeReleaseResult{
		ReclaimedCount: result.Count, ReclaimedAmount: result.Amount,
	}, nil
}
