package app

import (
	"fmt"
	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math"
	"testing"
	"time"
)

func TestOpenBlindBoxOrderByTradeNo_CreditsMatchingWallets(t *testing.T) {
	db := setupRedemptionTestDB(t)

	setting := blindboxsettings.Get()
	originalSetting := setting
	setting.Enabled = true
	setting.SubscriptionPrizeProbability = 0
	setting.PityThreshold = 999
	setting.PityGuaranteeUSD = 0
	setting.LowRewardThresholdUSD = 0
	setting.FirstPurchaseGuaranteeUSD = 0.0001
	t.Cleanup(func() {
		blindboxsettings.Set(originalSetting)
	})

	plan := &commerceschema.SubscriptionPlan{
		Id:               9509,
		Title:            setting.SubscriptionPlanTitle,
		Subtitle:         "盲盒月卡",
		PriceAmount:      99,
		Currency:         "CNY",
		DurationUnit:     commerceschema.SubscriptionDurationMonth,
		DurationValue:    1,
		Enabled:          true,
		TotalAmount:      100000,
		PeriodAmount:     100000,
		QuotaResetPeriod: commerceschema.SubscriptionResetMonthly,
	}
	require.NoError(t, db.Create(plan).Error)

	testCases := []struct {
		name             string
		userID           int
		username         string
		tradeNo          string
		rewardUSD        float64
		walletType       string
		expectedLabel    string
		expectedWallet   commerceschema.BlindBoxRewardWalletType
		expectedSnapshot string
	}{
		{
			name:             "default wallet reward",
			userID:           8803,
			username:         "blind_box_default_wallet_user",
			tradeNo:          "blind-box-default-wallet-order",
			rewardUSD:        1,
			walletType:       "default",
			expectedLabel:    "钱包：额度",
			expectedWallet:   commerceschema.BlindBoxRewardWalletTypeDefault,
			expectedSnapshot: "wallet",
		},
		{
			name:             "claude wallet reward",
			userID:           8804,
			username:         "blind_box_claude_wallet_user",
			tradeNo:          "blind-box-claude-wallet-order",
			rewardUSD:        2,
			walletType:       "claude",
			expectedLabel:    "钱包：Claude额度",
			expectedWallet:   commerceschema.BlindBoxRewardWalletTypeClaude,
			expectedSnapshot: "claude_wallet",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			caseSetting := setting
			caseSetting.Tiers = []blindboxsettings.TierSetting{
				{Name: tc.name, MinUSD: tc.rewardUSD, MaxUSD: tc.rewardUSD, Probability: 1, WalletType: tc.walletType},
			}
			blindboxsettings.Set(caseSetting)

			user := &identityschema.User{
				Id:          tc.userID,
				Username:    tc.username,
				Status:      constant.UserStatusEnabled,
				AffCode:     fmt.Sprintf("AFF-BLIND-%d", tc.userID),
				Quota:       100,
				ClaudeQuota: 200,
			}
			require.NoError(t, db.Create(user).Error)

			order := &commerceschema.BlindBoxOrder{
				UserId:          user.Id,
				Quantity:        1,
				Money:           5,
				TradeNo:         tc.tradeNo,
				PaymentMethod:   "test",
				PaymentProvider: "test",
				Status:          constant.TopUpStatusSuccess,
				CreateTime:      time.Now().Unix(),
			}
			require.NoError(t, db.Create(order).Error)

			records, err := OpenBlindBoxOrderByTradeNo(order.TradeNo)
			require.NoError(t, err)
			require.Len(t, records, 1)
			assert.Equal(t, string(tc.expectedWallet), records[0].RewardWalletType)

			expectedCredit := int(math.Round(tc.rewardUSD * platformruntime.QuotaPerUnit))
			var savedUser identityschema.User
			require.NoError(t, db.Where("id = ?", user.Id).First(&savedUser).Error)
			if tc.expectedWallet == commerceschema.BlindBoxRewardWalletTypeClaude {
				assert.Equal(t, 100, savedUser.Quota)
				assert.Equal(t, 200+expectedCredit, savedUser.ClaudeQuota)
				snapshot := loadCommerceBillingSnapshot(t, user.Id, tc.expectedSnapshot)
				assert.Equal(t, int64(savedUser.ClaudeQuota), snapshot.AvailableBalance)
			} else {
				assert.Equal(t, 100+expectedCredit, savedUser.Quota)
				assert.Equal(t, 200, savedUser.ClaudeQuota)
				snapshot := loadCommerceBillingSnapshot(t, user.Id, tc.expectedSnapshot)
				assert.Equal(t, int64(savedUser.Quota), snapshot.AvailableBalance)
			}

			var logs []auditschema.Log
			require.NoError(t, db.Where("user_id = ? AND type = ?", user.Id, auditschema.LogTypeTopup).Order("id asc").Find(&logs).Error)
			require.Len(t, logs, 1)
			assert.Contains(t, logs[0].Content, "盲盒开奖到账")
			assert.Contains(t, logs[0].Content, tc.expectedLabel)
			assert.Contains(t, logs[0].Content, fmt.Sprintf("开奖记录ID：%d", records[0].Id))
		})
	}
}

func TestOpenBlindBoxOrderByTradeNo_DoesNotDoubleCreditQuota(t *testing.T) {
	db := setupRedemptionTestDB(t)

	setting := blindboxsettings.Get()
	originalSetting := setting
	setting.Enabled = true
	setting.SubscriptionPrizeProbability = 0
	setting.PityThreshold = 999
	setting.PityGuaranteeUSD = 0
	setting.LowRewardThresholdUSD = 0
	setting.FirstPurchaseGuaranteeUSD = 0.0001
	setting.Tiers = []blindboxsettings.TierSetting{
		{Name: "quota-tier", MinUSD: 1, MaxUSD: 1, Probability: 1, WalletType: "default"},
	}
	blindboxsettings.Set(setting)
	t.Cleanup(func() {
		blindboxsettings.Set(originalSetting)
	})

	plan := &commerceschema.SubscriptionPlan{
		Id:               9510,
		Title:            setting.SubscriptionPlanTitle,
		Subtitle:         "盲盒月卡",
		PriceAmount:      99,
		Currency:         "CNY",
		DurationUnit:     commerceschema.SubscriptionDurationMonth,
		DurationValue:    1,
		Enabled:          true,
		TotalAmount:      100000,
		PeriodAmount:     100000,
		QuotaResetPeriod: commerceschema.SubscriptionResetMonthly,
	}
	require.NoError(t, db.Create(plan).Error)

	user := &identityschema.User{
		Id:       8813,
		Username: "blind_box_double_credit_user",
		Status:   constant.UserStatusEnabled,
		Quota:    100,
	}
	require.NoError(t, db.Create(user).Error)

	order := &commerceschema.BlindBoxOrder{
		UserId:          user.Id,
		Quantity:        1,
		OpenedCount:     0,
		Money:           5,
		TradeNo:         "blind-box-double-credit-order",
		PaymentMethod:   "test",
		PaymentProvider: "test",
		Status:          constant.TopUpStatusSuccess,
		CreateTime:      time.Now().Unix(),
		CompleteTime:    time.Now().Unix(),
	}
	require.NoError(t, db.Create(order).Error)

	beforeQuota := user.Quota
	records1, err := OpenBlindBoxOrderByTradeNo(order.TradeNo)
	require.NoError(t, err)
	require.Len(t, records1, 1)

	var afterFirst identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&afterFirst).Error)
	assert.Greater(t, afterFirst.Quota, beforeQuota)

	records2, err := OpenBlindBoxOrderByTradeNo(order.TradeNo)
	require.NoError(t, err)
	assert.Len(t, records2, 0)

	var afterSecond identityschema.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&afterSecond).Error)
	assert.Equal(t, afterFirst.Quota, afterSecond.Quota)

	snapshot := loadCommerceBillingSnapshot(t, user.Id, "wallet")
	assert.Equal(t, int64(afterFirst.Quota), snapshot.AvailableBalance)

	var savedOrder commerceschema.BlindBoxOrder
	require.NoError(t, db.Where("id = ?", order.Id).First(&savedOrder).Error)
	assert.Equal(t, 1, savedOrder.OpenedCount)

	var logs []auditschema.Log
	require.NoError(t, db.Where("user_id = ? AND type = ?", user.Id, auditschema.LogTypeTopup).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0].Content, "盲盒开奖到账")
}

func TestOpenBlindBoxOrderByTradeNo_PropRewardAdvancesPityAndThresholdDrawTriggersGuarantee(t *testing.T) {
	db := setupRedemptionTestDB(t)

	setting := blindboxsettings.Get()
	originalSetting := setting
	setting.Enabled = true
	setting.SubscriptionPrizeProbability = 0
	setting.PityThreshold = 2
	setting.PityGuaranteeUSD = 2
	setting.LowRewardThresholdUSD = 2
	setting.FirstPurchaseGuaranteeUSD = 0.0001
	setting.Tiers = []blindboxsettings.TierSetting{
		{Name: "0.9 倍率卡", Probability: 1, RewardType: commerceschema.BlindBoxRewardTypeProp},
	}
	blindboxsettings.Set(setting)
	t.Cleanup(func() {
		blindboxsettings.Set(originalSetting)
	})

	user := &identityschema.User{
		Id:       8814,
		Username: "blind_box_prop_pity_user",
		Status:   constant.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	order := &commerceschema.BlindBoxOrder{
		UserId:          user.Id,
		Quantity:        2,
		TradeNo:         "blind-box-prop-pity-order",
		PaymentMethod:   "test",
		PaymentProvider: "test",
		Status:          constant.TopUpStatusSuccess,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, db.Create(order).Error)

	firstDraw, err := OpenBlindBoxes(user.Id, 1)
	require.NoError(t, err)
	require.Len(t, firstDraw, 1)
	assert.Equal(t, commerceschema.BlindBoxRewardTypeProp, firstDraw[0].RewardType)
	assert.False(t, firstDraw[0].IsPity)

	var pityState commerceschema.BlindBoxPityState
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&pityState).Error)
	assert.Equal(t, 1, pityState.ConsecutiveLowRewards)

	secondDraw, err := OpenBlindBoxes(user.Id, 1)
	require.NoError(t, err)
	require.Len(t, secondDraw, 1)
	assert.True(t, secondDraw[0].IsPity)
	assert.Equal(t, 2.0, secondDraw[0].RewardUSD)

	require.NoError(t, db.Where("user_id = ?", user.Id).First(&pityState).Error)
	assert.Zero(t, pityState.ConsecutiveLowRewards)
}

func TestApplyFirstPurchaseMinimumGuarantee(t *testing.T) {
	tests := []struct {
		name               string
		isFirstPurchase    bool
		rewardUSD          float64
		rewardType         string
		walletType         commerceschema.BlindBoxRewardWalletType
		expectedRewardUSD  float64
		expectedRewardType string
		expectedWalletType commerceschema.BlindBoxRewardWalletType
	}{
		{
			name:               "raises low ordinary quota",
			isFirstPurchase:    true,
			rewardUSD:          5,
			rewardType:         commerceschema.BlindBoxRewardTypeQuota,
			walletType:         commerceschema.BlindBoxRewardWalletTypeDefault,
			expectedRewardUSD:  20,
			expectedRewardType: commerceschema.BlindBoxRewardTypeQuota,
			expectedWalletType: commerceschema.BlindBoxRewardWalletTypeDefault,
		},
		{
			name:               "raises low claude quota",
			isFirstPurchase:    true,
			rewardUSD:          2,
			rewardType:         commerceschema.BlindBoxRewardTypeClaudeQuota,
			walletType:         commerceschema.BlindBoxRewardWalletTypeClaude,
			expectedRewardUSD:  5,
			expectedRewardType: commerceschema.BlindBoxRewardTypeClaudeQuota,
			expectedWalletType: commerceschema.BlindBoxRewardWalletTypeClaude,
		},
		{
			name:               "converts prop reward to ordinary guarantee",
			isFirstPurchase:    true,
			rewardUSD:          0,
			rewardType:         commerceschema.BlindBoxRewardTypeProp,
			walletType:         commerceschema.BlindBoxRewardWalletTypeDefault,
			expectedRewardUSD:  20,
			expectedRewardType: commerceschema.BlindBoxRewardTypeQuota,
			expectedWalletType: commerceschema.BlindBoxRewardWalletTypeDefault,
		},
		{
			name:               "keeps high claude quota",
			isFirstPurchase:    true,
			rewardUSD:          25,
			rewardType:         commerceschema.BlindBoxRewardTypeClaudeQuota,
			walletType:         commerceschema.BlindBoxRewardWalletTypeClaude,
			expectedRewardUSD:  25,
			expectedRewardType: commerceschema.BlindBoxRewardTypeClaudeQuota,
			expectedWalletType: commerceschema.BlindBoxRewardWalletTypeClaude,
		},
		{
			name:               "skips non first purchase",
			isFirstPurchase:    false,
			rewardUSD:          5,
			rewardType:         commerceschema.BlindBoxRewardTypeQuota,
			walletType:         commerceschema.BlindBoxRewardWalletTypeDefault,
			expectedRewardUSD:  5,
			expectedRewardType: commerceschema.BlindBoxRewardTypeQuota,
			expectedWalletType: commerceschema.BlindBoxRewardWalletTypeDefault,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewardUSD := tt.rewardUSD
			rewardType := tt.rewardType
			walletType := tt.walletType

			applyFirstPurchaseMinimumGuarantee(
				tt.isFirstPurchase,
				20,
				5,
				&rewardUSD,
				&rewardType,
				&walletType,
			)

			assert.Equal(t, tt.expectedRewardUSD, rewardUSD)
			assert.Equal(t, tt.expectedRewardType, rewardType)
			assert.Equal(t, tt.expectedWalletType, walletType)
		})
	}
}
