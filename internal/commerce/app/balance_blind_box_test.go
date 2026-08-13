package app

import (
	"errors"
	"math"
	"testing"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
)

func TestOpenBalanceBlindBoxDebitsOnceAndDoesNotIssueLuckyNumber(t *testing.T) {
	db := setupRedemptionTestDB(t)
	original := blindboxsettings.Get()
	setting := original
	setting.Enabled = true
	setting.BalanceBlindBoxEnabled = true
	setting.BalanceBlindBoxPriceUSD = 15
	setting.BalanceBlindBoxPityThreshold = 50
	setting.BalanceBlindBoxSmallPityThreshold = 10
	setting.BalanceBlindBoxTiers = []blindboxsettings.TierSetting{{
		Name: "$3 普通额度", MinUSD: 3, MaxUSD: 3, Probability: 1,
		RewardType: commerceschema.BlindBoxRewardTypeQuota, WalletType: "default",
	}}
	blindboxsettings.Set(setting)
	t.Cleanup(func() { blindboxsettings.Set(original) })

	startingQuota := int(math.Round(100 * platformruntime.QuotaPerUnit))
	user := &identityschema.User{Id: 8891, Username: "balance_blind_box_user", Quota: startingQuota, AffCode: "balance-box-user"}
	require.NoError(t, db.Create(user).Error)

	first, err := OpenBalanceBlindBox(user.Id, "balance-box-request-1")
	require.NoError(t, err)
	require.Equal(t, commerceschema.BlindBoxPoolTypeBalance15, first.Record.PoolType)
	require.Equal(t, 10.0, first.Record.RewardUSD)
	require.Empty(t, first.Record.LuckyNumber)
	require.InDelta(t, 95.0, first.BalanceUSD, 0.000001)

	second, err := OpenBalanceBlindBox(user.Id, "balance-box-request-1")
	require.NoError(t, err)
	require.Equal(t, first.Record.Id, second.Record.Id)
	require.InDelta(t, 95.0, second.BalanceUSD, 0.000001)

	var luckyCount int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxDailyLuckyNumber{}).Count(&luckyCount).Error)
	require.Zero(t, luckyCount)
	var recordCount int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxOpenRecord{}).Where("pool_type = ?", commerceschema.BlindBoxPoolTypeBalance15).Count(&recordCount).Error)
	require.Equal(t, int64(1), recordCount)
}

func TestOpenBalanceBlindBoxSupportsBatchOpening(t *testing.T) {
	db := setupRedemptionTestDB(t)
	original := blindboxsettings.Get()
	setting := original
	setting.Enabled = true
	setting.BalanceBlindBoxEnabled = true
	setting.BalanceBlindBoxPriceUSD = 15
	setting.BalanceBlindBoxTiers = []blindboxsettings.TierSetting{{Name: "$1-$3 普通额度", MinUSD: 1, MaxUSD: 3, Probability: 1, RewardType: "quota", WalletType: "default"}}
	blindboxsettings.Set(setting)
	t.Cleanup(func() { blindboxsettings.Set(original) })
	user := &identityschema.User{Id: 8895, Username: "balance_blind_box_batch", Quota: int(100 * platformruntime.QuotaPerUnit), AffCode: "balance-box-batch"}
	require.NoError(t, db.Create(user).Error)
	result, err := OpenBalanceBlindBox(user.Id, "balance-box-request-batch", 3)
	require.NoError(t, err)
	require.Len(t, result.Records, 3)
	require.GreaterOrEqual(t, result.BalanceUSD, 68.0)
	require.LessOrEqual(t, result.BalanceUSD, 71.0)
	for _, record := range result.Records {
		require.Equal(t, commerceschema.BlindBoxPoolTypeBalance15, record.PoolType)
		require.Empty(t, record.LuckyNumber)
	}
}

func TestOpenBalanceBlindBoxRejectsInsufficientBalanceWithoutRecord(t *testing.T) {
	db := setupRedemptionTestDB(t)
	original := blindboxsettings.Get()
	setting := original
	setting.Enabled = true
	setting.BalanceBlindBoxEnabled = true
	setting.BalanceBlindBoxPriceUSD = 15
	blindboxsettings.Set(setting)
	t.Cleanup(func() { blindboxsettings.Set(original) })

	user := &identityschema.User{Id: 8892, Username: "balance_blind_box_low_balance", Quota: int(14 * platformruntime.QuotaPerUnit), AffCode: "balance-box-low"}
	require.NoError(t, db.Create(user).Error)

	_, err := OpenBalanceBlindBox(user.Id, "balance-box-request-low")
	require.Error(t, err)
	require.False(t, errors.Is(err, billingdomain.ErrInsufficientBalance))
	var recordCount int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxOpenRecord{}).Count(&recordCount).Error)
	require.Zero(t, recordCount)
}

func TestBalanceBlindBoxFiftiethDrawUsesIndependentGuarantee(t *testing.T) {
	db := setupRedemptionTestDB(t)
	original := blindboxsettings.Get()
	setting := original
	setting.Enabled = true
	setting.BalanceBlindBoxEnabled = true
	setting.BalanceBlindBoxPriceUSD = 15
	setting.BalanceBlindBoxPityThreshold = 50
	setting.BalanceBlindBoxSmallPityThreshold = 10
	setting.BalanceBlindBoxPityGuaranteeUSD = 35
	setting.BalanceBlindBoxTiers = []blindboxsettings.TierSetting{{
		Name: "$1.5 普通额度", MinUSD: 1.5, MaxUSD: 1.5, Probability: 1,
		RewardType: commerceschema.BlindBoxRewardTypeQuota, WalletType: "default",
	}}
	blindboxsettings.Set(setting)
	t.Cleanup(func() { blindboxsettings.Set(original) })

	user := &identityschema.User{Id: 8893, Username: "balance_blind_box_pity", Quota: int(100 * platformruntime.QuotaPerUnit), AffCode: "balance-box-pity"}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(&commerceschema.BalanceBlindBoxPityState{UserId: user.Id, ConsecutiveUnder35USD: 49}).Error)

	result, err := OpenBalanceBlindBox(user.Id, "balance-box-request-pity")
	require.NoError(t, err)
	require.True(t, result.Record.IsPity)
	require.Equal(t, 35.0, result.Record.RewardUSD)
	require.Zero(t, result.Overview.PityProgress)
}

func TestBalanceBlindBoxTenthDrawUsesSmallGuarantee(t *testing.T) {
	db := setupRedemptionTestDB(t)
	original := blindboxsettings.Get()
	setting := original
	setting.Enabled = true
	setting.BalanceBlindBoxEnabled = true
	setting.BalanceBlindBoxPriceUSD = 15
	setting.BalanceBlindBoxPityThreshold = 50
	setting.BalanceBlindBoxSmallPityThreshold = 10
	setting.BalanceBlindBoxSmallPityGuaranteeUSD = 10
	setting.BalanceBlindBoxTiers = []blindboxsettings.TierSetting{{Name: "$1.5 普通额度", MinUSD: 1.5, MaxUSD: 1.5, Probability: 1, RewardType: "quota", WalletType: "default"}}
	blindboxsettings.Set(setting)
	t.Cleanup(func() { blindboxsettings.Set(original) })

	user := &identityschema.User{Id: 8894, Username: "balance_blind_box_small_pity", Quota: int(200 * platformruntime.QuotaPerUnit), AffCode: "balance-box-small-pity"}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(&commerceschema.BalanceBlindBoxPityState{UserId: user.Id, ConsecutiveUnder6USD: 9, ConsecutiveUnder35USD: 9}).Error)

	result, err := OpenBalanceBlindBox(user.Id, "balance-box-request-small-pity")
	require.NoError(t, err)
	require.True(t, result.Record.IsPity)
	require.Equal(t, 10.0, result.Record.RewardUSD)
	require.Equal(t, 0, result.Overview.SmallPityProgress)
	require.Equal(t, 10, result.Overview.PityProgress)
}

func TestBalanceBlindBoxFirstDrawUsesGuarantee(t *testing.T) {
	db := setupRedemptionTestDB(t)
	require.NoError(t, db.AutoMigrate(&commerceschema.BalanceBlindBoxPityState{}))
	setting := blindboxsettings.Get()
	setting.Enabled = true
	setting.BalanceBlindBoxEnabled = true
	setting.BalanceBlindBoxPriceUSD = 15
	setting.BalanceBlindBoxFirstDrawGuaranteeUSD = 10
	setting.BalanceBlindBoxTiers = []blindboxsettings.TierSetting{{Name: "$1-$3", MinUSD: 1, MaxUSD: 3, Probability: 1, RewardType: "quota", WalletType: "default"}}
	blindboxsettings.Set(setting)

	user := &identityschema.User{Id: 8896, Username: "balance-blind-box-first", Quota: int(50 * platformruntime.QuotaPerUnit), AffCode: "balance-box-first"}
	require.NoError(t, db.Create(user).Error)

	result, err := OpenBalanceBlindBox(user.Id, "balance-box-first-request")
	require.NoError(t, err)
	require.Len(t, result.Records, 1)
	require.Equal(t, 10.0, result.Records[0].RewardUSD)
	require.True(t, result.Records[0].IsPity)
	require.False(t, result.Overview.FirstDrawEligible)
}
