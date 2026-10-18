package app

import (
	"encoding/json"
	"fmt"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	"slices"
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
	normalizedSearch := normalizeExternalIDSearch(input.OwnerSearch)
	cacheKey := fmt.Sprintf("%p:%d:%s:%d:%d", platformdb.DB, marketplacePublicationRevision.Load(), normalizedSearch, input.StartTimestamp, input.EndTimestamp)
	value, err, _ := adminOwnerIncomeLoads.Do(cacheKey, func() (any, error) {
		return loadAdminOwnerIncome(input, normalizedSearch, cacheKey)
	})
	if err != nil {
		return nil, err
	}
	// Each caller gets an independent result and fresh spendable wallet balances.
	return populateAdminOwnerBalances(cloneAdminOwnerIncomeResult(value.(*AdminOwnerIncomeResult)))
}

func loadAdminOwnerIncome(input AdminOwnerIncomeQuery, normalizedSearch, cacheKey string) (*AdminOwnerIncomeResult, error) {
	adminMarketplaceStatsCache.Lock()
	if adminMarketplaceStatsCache.ownerIncomeResult != nil &&
		adminMarketplaceStatsCache.ownerIncomeKey == cacheKey &&
		time.Since(adminMarketplaceStatsCache.ownerIncomeAt) < adminMarketplaceStatsCacheTTL {
		result := cloneAdminOwnerIncomeResult(adminMarketplaceStatsCache.ownerIncomeResult)
		adminMarketplaceStatsCache.Unlock()
		return result, nil
	}
	adminMarketplaceStatsCache.Unlock()
	query, err := incomeFacts(platformdb.DB, input.StartTimestamp, input.EndTimestamp)
	if err != nil {
		return nil, err
	}
	query = query.Select("owner_user_id, " + incomeSums)
	if normalizedSearch != "" {
		ownerUserIDs, err := ownerUserIDsByExternalID(normalizedSearch)
		if err != nil {
			return nil, err
		}
		if len(ownerUserIDs) == 0 {
			result := &AdminOwnerIncomeResult{Items: []AdminOwnerIncomeItem{}}
			cacheAdminOwnerIncomeResult(cacheKey, result)
			return result, nil
		}
		query = query.Where("owner_user_id IN ?", ownerUserIDs)
	}

	result := &AdminOwnerIncomeResult{Items: []AdminOwnerIncomeItem{}}
	if err := query.Group("owner_user_id").Having("SUM(request_count) > 0").Order("total_income DESC, owner_user_id ASC").Scan(&result.Items).Error; err != nil {
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
	cacheAdminOwnerIncomeResult(cacheKey, result)
	return result, nil
}

func populateAdminOwnerBalances(result *AdminOwnerIncomeResult) (*AdminOwnerIncomeResult, error) {
	ids := make([]int, 0, len(result.Items))
	for _, item := range result.Items {
		ids = append(ids, item.OwnerUserID)
	}
	balances, err := billingapp.GetUserClaudeWalletQuotas(ids)
	if err != nil {
		return nil, err
	}
	for index := range result.Items {
		item := &result.Items[index]
		item.CurrentQuota = balances[item.OwnerUserID]
		item.ReclaimableQuota = max(0, min(item.CurrentQuota, item.ReleasedIncome))
	}
	return result, nil
}

func cacheAdminOwnerIncomeResult(cacheKey string, result *AdminOwnerIncomeResult) {
	adminMarketplaceStatsCache.Lock()
	adminMarketplaceStatsCache.ownerIncomeAt = time.Now()
	adminMarketplaceStatsCache.ownerIncomeKey = cacheKey
	adminMarketplaceStatsCache.ownerIncomeResult = cloneAdminOwnerIncomeResult(result)
	adminMarketplaceStatsCache.Unlock()
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
		if ownerIDs == nil {
			ownerIDs = input.OwnerUserIDs
		} else {
			ownerIDs = slices.DeleteFunc(ownerIDs, func(id int) bool { return !slices.Contains(input.OwnerUserIDs, id) })
			if len(ownerIDs) == 0 {
				return &AdminOwnerIncomeReleaseResult{}, nil
			}
		}
	}
	task, err := marketplacesettlement.CreateIncomeReclaimTask(marketplacesettlement.ReleaseFilter{
		OwnerUserIDs: ownerIDs, StartTimestamp: input.StartTimestamp,
		EndTimestamp: input.EndTimestamp,
		MaxAmount:    input.MaxAmount,
		OperationID:  input.OperationID,
		Direct:       true,
	})
	if err != nil {
		return nil, err
	}
	return adminOwnerIncomeReclaimResult(task)
}

func GetAdminOwnerIncomeReclaim(operationID string) (*AdminOwnerIncomeReleaseResult, error) {
	task, err := marketplacesettlement.GetIncomeReclaimTask(operationID)
	if err != nil {
		return nil, err
	}
	if task.Status == "completed" {
		invalidateAdminMarketplaceStatsCache()
	}
	return adminOwnerIncomeReclaimResult(task)
}

func adminOwnerIncomeReclaimResult(task marketplaceschema.IncomeReclaim) (*AdminOwnerIncomeReleaseResult, error) {
	var filter marketplacesettlement.ReleaseFilter
	if err := json.Unmarshal([]byte(task.Filter), &filter); err != nil {
		return nil, fmt.Errorf("decode income reclaim task filter: %w", err)
	}
	status := task.Status
	if status == "debit_pending" {
		status = "pending"
	}
	if status == "charged" {
		status = "completed"
	}
	amounts := map[int]int64{}
	if task.OwnerAmounts != "" {
		if err := json.Unmarshal([]byte(task.OwnerAmounts), &amounts); err != nil {
			return nil, fmt.Errorf("decode income reclaim owner amounts: %w", err)
		}
	}
	ownerIDs := make([]int, 0, len(amounts))
	for ownerID := range amounts {
		ownerIDs = append(ownerIDs, ownerID)
	}
	externalIDs, err := ownerExternalIDs(ownerIDs)
	if err != nil {
		return nil, err
	}
	items := make([]AdminOwnerIncomeReleaseItem, 0, len(amounts))
	for ownerID, amount := range amounts {
		items = append(items, AdminOwnerIncomeReleaseItem{OwnerUserID: ownerID, OwnerExternalID: externalIDs[ownerID], Amount: amount})
	}
	slices.SortFunc(items, func(a, b AdminOwnerIncomeReleaseItem) int { return a.OwnerUserID - b.OwnerUserID })
	return &AdminOwnerIncomeReleaseResult{
		OperationID: task.ID, Status: status, ReclaimedCount: task.Count,
		Direct: filter.Direct, AuditPending: task.Status == "charged",
		ReclaimedAmount: task.Amount, TargetAmount: filter.MaxAmount, Items: items,
		BatchNumber: task.BatchNumber, ErrorMessage: task.ErrorMessage,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}, nil
}
