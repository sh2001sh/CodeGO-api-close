package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"

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
	return loadChannelEarnings(0, ids, startTimestamp, endTimestamp)
}

func earningsByOwnerGroupIDs(ownerUserID int, ids []string) (map[string]ownerChannelEarnings, error) {
	if ownerUserID <= 0 {
		return nil, errors.New("用户未登录")
	}
	return loadChannelEarnings(ownerUserID, ids, 0, 0)
}

func loadChannelEarnings(ownerUserID int, ids []string, startTimestamp, endTimestamp int64) (map[string]ownerChannelEarnings, error) {
	result := make(map[string]ownerChannelEarnings, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	cacheIDs := slices.Clone(ids)
	slices.Sort(cacheIDs)
	cacheKey := fmt.Sprintf("%p:%d:%d:%d:%s", platformdb.DB, ownerUserID, startTimestamp, endTimestamp, strings.Join(cacheIDs, ","))
	// Owner reads share only overlapping work. Settlements and reclaim updates
	// must be visible on the next request, independently of the admin cache.
	if ownerUserID == 0 {
		if cached, ok := cachedChannelEarnings(cacheKey); ok {
			return cached, nil
		}
	}
	value, err, _ := channelEarningsLoads.Do(cacheKey, func() (any, error) {
		if ownerUserID == 0 {
			if cached, ok := cachedChannelEarnings(cacheKey); ok {
				return cached, nil
			}
		}
		loaded := make(map[string]ownerChannelEarnings, len(ids))
		var rows []ownerChannelEarnings
		query, err := incomeFacts(platformdb.DB, startTimestamp, endTimestamp)
		if err != nil {
			return nil, err
		}
		query = query.Select("group_id, "+incomeSums).Where("group_id IN ?", ids)
		if ownerUserID > 0 {
			query = query.Where("owner_user_id = ?", ownerUserID)
		}
		if err := query.Group("group_id").Having("SUM(request_count) > 0").Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			loaded[row.GroupID] = row
		}
		if ownerUserID == 0 {
			cacheChannelEarnings(cacheKey, loaded)
		}
		return loaded, nil
	})
	if err != nil {
		return nil, err
	}
	return cloneChannelEarnings(value.(map[string]ownerChannelEarnings)), nil
}
