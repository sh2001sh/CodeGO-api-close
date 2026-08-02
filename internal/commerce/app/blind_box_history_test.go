package app

import (
	"testing"
	"time"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
)

func TestListBlindBoxHistoryFiltersThirtyDaysAndNamesProps(t *testing.T) {
	db := setupRedemptionTestDB(t)
	now := platformruntime.GetTimestamp()
	recent := commerceschema.BlindBoxOpenRecord{
		UserId:      301,
		RewardType:  commerceschema.BlindBoxRewardTypeProp,
		RewardTitle: "实用道具奖励",
		RewardTier:  "prop",
		CreateTime:  now - int64(2*time.Hour/time.Second),
	}
	require.NoError(t, db.Create(&recent).Error)
	require.NoError(t, db.Create(&commerceschema.BlindBoxProp{
		UserId:       recent.UserId,
		OpenRecordId: recent.Id,
		PropType:     commerceschema.BlindBoxPropTypeSubscriptionDiscount90,
		Title:        "套餐九折卡",
		Status:       commerceschema.BlindBoxPropStatusAvailable,
	}).Error)
	require.NoError(t, db.Create(&commerceschema.BlindBoxOpenRecord{
		UserId:      recent.UserId,
		RewardType:  commerceschema.BlindBoxRewardTypeQuota,
		RewardTitle: "过期历史",
		CreateTime:  now - int64(31*24*time.Hour/time.Second),
	}).Error)
	require.NoError(t, db.Create(&commerceschema.BlindBoxOpenRecord{
		UserId:      302,
		RewardType:  commerceschema.BlindBoxRewardTypeQuota,
		RewardTitle: "其他用户记录",
		CreateTime:  now,
	}).Error)

	page, err := ListBlindBoxHistory(recent.UserId, 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Equal(t, blindBoxHistoryRetentionDays, page.RetentionDays)
	require.Len(t, page.Records, 1)
	require.Equal(t, "套餐九折卡", page.Records[0].RewardTitle)
	require.Equal(t, commerceschema.BlindBoxPropTypeSubscriptionDiscount90, page.Records[0].PropType)
	require.Equal(t, commerceschema.BlindBoxPropStatusAvailable, page.Records[0].PropStatus)
}

func TestListBlindBoxHistoryPaginatesNewestFirst(t *testing.T) {
	db := setupRedemptionTestDB(t)
	now := platformruntime.GetTimestamp()
	for index := 1; index <= 3; index++ {
		require.NoError(t, db.Create(&commerceschema.BlindBoxOpenRecord{
			UserId:      303,
			RewardType:  commerceschema.BlindBoxRewardTypeQuota,
			RewardTitle: "额度奖励",
			CreateTime:  now + int64(index),
		}).Error)
	}

	page, err := ListBlindBoxHistory(303, 2, 2)
	require.NoError(t, err)
	require.EqualValues(t, 3, page.Total)
	require.Len(t, page.Records, 1)
	require.Equal(t, now+1, page.Records[0].CreateTime)
}

func TestGetBlindBoxStatisticsAggregatesAllOpeningRecords(t *testing.T) {
	db := setupRedemptionTestDB(t)
	userID := 304
	records := []commerceschema.BlindBoxOpenRecord{
		{UserId: userID, RewardType: commerceschema.BlindBoxRewardTypeQuota, RewardUSD: 5, CreditAmount: 5_000_000, IsPity: true},
		{UserId: userID, RewardType: commerceschema.BlindBoxRewardTypeQuota, RewardUSD: 3, CreditAmount: 3_000_000},
		{UserId: userID, RewardType: commerceschema.BlindBoxRewardTypeClaudeQuota, RewardUSD: 8, CreditAmount: 8_000_000},
		{UserId: userID, RewardType: commerceschema.BlindBoxRewardTypeProp},
		{UserId: 305, RewardType: commerceschema.BlindBoxRewardTypeQuota, RewardUSD: 99},
	}
	for index := range records {
		require.NoError(t, db.Create(&records[index]).Error)
	}

	statistics, err := GetBlindBoxStatistics(userID)
	require.NoError(t, err)
	require.EqualValues(t, 4, statistics.TotalOpened)
	require.EqualValues(t, 1, statistics.PityWins)
	require.Len(t, statistics.Rewards, 3)
	require.Equal(t, commerceschema.BlindBoxRewardTypeClaudeQuota, statistics.Rewards[0].RewardType)
	require.EqualValues(t, 1, statistics.Rewards[0].OpenedCount)
	require.Equal(t, 8.0, statistics.Rewards[0].RewardUSD)
	require.Equal(t, commerceschema.BlindBoxRewardTypeProp, statistics.Rewards[1].RewardType)
	require.Equal(t, commerceschema.BlindBoxRewardTypeQuota, statistics.Rewards[2].RewardType)
	require.EqualValues(t, 2, statistics.Rewards[2].OpenedCount)
	require.Equal(t, 8.0, statistics.Rewards[2].RewardUSD)
}
