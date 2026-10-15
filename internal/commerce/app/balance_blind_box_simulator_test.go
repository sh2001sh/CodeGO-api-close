package app

import (
	"fmt"
	"testing"

	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
)

func TestFormalAndSimulatedBalanceBlindBoxesMatchGuarantees(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	setting := blindboxsettings.Get()
	setting.BalanceBlindBoxTiers = fixedGuaranteePool("normal", 1.25)
	setting.BalanceBlindBoxFirstDrawTiers = fixedGuaranteePool("first", 2.75)
	setting.BalanceBlindBoxSmallPityTiers = fixedGuaranteePool("small", 3.25)
	setting.BalanceBlindBoxPityTiers = fixedGuaranteePool("big", 9.25)
	blindboxsettings.Set(setting)
	unit := int64(platformruntime.QuotaPerUnit)

	cases := []struct {
		name, guarantee         string
		first                   bool
		smallProgress, progress int
	}{
		{name: "ordinary", guarantee: balanceBlindBoxGuaranteeNone},
		{name: "first", guarantee: balanceBlindBoxGuaranteeFirst, first: true},
		{name: "small", guarantee: balanceBlindBoxGuaranteeSmall, smallProgress: setting.BalanceBlindBoxSmallPityThreshold - 1},
		{name: "big", guarantee: balanceBlindBoxGuaranteeBig, progress: setting.BalanceBlindBoxPityThreshold - 1},
	}
	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			user := createBalanceBlindBoxTestUser(t, db, 9100+index, fmt.Sprintf("SIM%03d", index), 100)
			if !testCase.first {
				require.NoError(t, db.Create(&commerceschema.BlindBoxOpenRecord{UserId: user.Id, PoolType: commerceschema.BlindBoxPoolTypeUnified}).Error)
			}
			require.NoError(t, db.Create(&commerceschema.BalanceBlindBoxPityState{UserId: user.Id, ConsecutiveUnder6USD: testCase.smallProgress, ConsecutiveUnder35USD: testCase.progress}).Error)
			_, err := PurchaseBalanceBlindBoxes(user.Id, "compare-purchase-"+testCase.name, 1)
			require.NoError(t, err)
			formal, err := OpenBalanceBlindBox(user.Id, "compare-open-"+testCase.name, 1)
			require.NoError(t, err)
			var formalItem commerceschema.BalanceBlindBoxItem
			require.NoError(t, db.Where("open_record_id = ?", formal.Record.Id).First(&formalItem).Error)
			simulated, err := SimulateBalanceBlindBoxes(100*unit, 1, BalanceBlindBoxSimulationState{SmallPityProgress: testCase.smallProgress, PityProgress: testCase.progress, FirstDrawEligible: testCase.first})
			require.NoError(t, err)
			require.Equal(t, testCase.guarantee, formalItem.GuaranteeType)
			require.Equal(t, formalItem.GuaranteeType, simulated.Draws[0].GuaranteeType)
			require.Equal(t, formal.Record.RewardTier, simulated.Draws[0].RewardTier)
			require.Equal(t, formal.Record.RewardUSD, simulated.Draws[0].RewardUSD)
			require.Equal(t, formal.Overview.SmallPityProgress, simulated.SmallPityProgress)
			require.Equal(t, formal.Overview.PityProgress, simulated.PityProgress)
		})
	}
}

func TestSimulateBalanceBlindBoxesUsesUnifiedPoolWithoutNegativeBalance(t *testing.T) {
	setBalanceBlindBoxTestSetting(t, 100)
	unit := int64(platformruntime.QuotaPerUnit)
	result, err := SimulateBalanceBlindBoxes(100*unit, 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Draws), 10)
	require.Equal(t, result.BalanceBefore-result.CostQuota+result.RewardQuota, result.BalanceAfter)
	require.GreaterOrEqual(t, result.BalanceAfter, int64(0))
	for _, draw := range result.Draws {
		require.NotEmpty(t, draw.RewardType)
		require.NotEmpty(t, draw.RewardTier)
		require.NotEmpty(t, draw.RewardTitle)
	}
}

func TestSimulateBalanceBlindBoxesOpensExtraDrawReward(t *testing.T) {
	setBalanceBlindBoxTestSetting(t, 10)
	setting := blindboxsettings.Get()
	setting.BalanceBlindBoxTiers = fixedGuaranteePool("next", 1)
	setting.BalanceBlindBoxFirstDrawTiers = []blindboxsettings.TierSetting{{
		Name: "再来一抽", Probability: 1, RewardType: "prop",
	}}
	blindboxsettings.Set(setting)
	unit := int64(platformruntime.QuotaPerUnit)

	result, err := SimulateBalanceBlindBoxes(100*unit, 1)
	require.NoError(t, err)
	require.Len(t, result.Draws, 2)
	require.Equal(t, "再来一抽", result.Draws[0].RewardTitle)
	require.Equal(t, "next", result.Draws[1].RewardTier)
	require.Equal(t, quotaUnitsFromBlindBoxUSD(1), result.RewardQuota)
	require.Equal(t, quotaUnitsFromBlindBoxUSD(2.5), result.CostQuota)
}

func TestFormalAndSimulatedBalanceBlindBoxesMatchExtraDraw(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	setting := blindboxsettings.Get()
	setting.BalanceBlindBoxTiers = fixedGuaranteePool("normal-after-extra", 1.25)
	setting.BalanceBlindBoxFirstDrawTiers = []blindboxsettings.TierSetting{{
		Name: "再来一抽", Probability: 1, RewardType: commerceschema.BlindBoxRewardTypeProp,
	}}
	blindboxsettings.Set(setting)
	user := createBalanceBlindBoxTestUser(t, db, 9110, "SIM010", 100)
	_, err := PurchaseBalanceBlindBoxes(user.Id, "compare-extra-purchase", 1)
	require.NoError(t, err)
	formalExtra, err := OpenBalanceBlindBox(user.Id, "compare-extra-open", 1)
	require.NoError(t, err)
	var formalExtraItem commerceschema.BalanceBlindBoxItem
	require.NoError(t, db.Where("open_record_id = ?", formalExtra.Record.Id).First(&formalExtraItem).Error)
	formalReward, err := OpenBalanceBlindBox(user.Id, "compare-extra-reward", 1)
	require.NoError(t, err)

	unit := int64(platformruntime.QuotaPerUnit)
	simulated, err := SimulateBalanceBlindBoxes(100*unit, 1)
	require.NoError(t, err)
	require.Len(t, simulated.Draws, 2)
	require.Equal(t, formalExtra.Record.RewardTitle, simulated.Draws[0].RewardTitle)
	require.Equal(t, formalExtraItem.GuaranteeType, simulated.Draws[0].GuaranteeType)
	require.Equal(t, formalReward.Record.RewardTier, simulated.Draws[1].RewardTier)
	require.Equal(t, formalReward.Record.RewardUSD, simulated.Draws[1].RewardUSD)
	require.Equal(t, formalReward.Overview.SmallPityProgress, simulated.SmallPityProgress)
	require.Equal(t, formalReward.Overview.PityProgress, simulated.PityProgress)
}

func TestSimulateBalanceBlindBoxesRejectsInsufficientOrExcessiveInput(t *testing.T) {
	setBalanceBlindBoxTestSetting(t, 100)
	unit := int64(platformruntime.QuotaPerUnit)
	_, err := SimulateBalanceBlindBoxes(2*unit, 1)
	require.Error(t, err)
	_, err = SimulateBalanceBlindBoxes(100*unit, 101)
	require.Error(t, err)
	_, err = SimulateBalanceBlindBoxes((maxBlindBoxSimulationBalanceUSD+1)*unit, 1)
	require.Error(t, err)
}

func TestSimulateBalanceBlindBoxesPreservesFirstDrawAndPityState(t *testing.T) {
	setBalanceBlindBoxTestSetting(t, 100)
	unit := int64(platformruntime.QuotaPerUnit)

	first, err := SimulateBalanceBlindBoxes(100*unit, 1)
	require.NoError(t, err)
	require.Equal(t, balanceBlindBoxGuaranteeFirst, first.Draws[0].GuaranteeType)
	require.False(t, first.FirstDrawEligible)

	next, err := SimulateBalanceBlindBoxes(first.BalanceAfter, 1, BalanceBlindBoxSimulationState{
		SmallPityProgress: first.SmallPityProgress,
		PityProgress:      first.PityProgress,
		FirstDrawEligible: first.FirstDrawEligible,
	})
	require.NoError(t, err)
	require.Equal(t, balanceBlindBoxGuaranteeNone, next.Draws[0].GuaranteeType)

	setting := blindboxsettings.Get()
	small, err := SimulateBalanceBlindBoxes(100*unit, 1, BalanceBlindBoxSimulationState{
		SmallPityProgress: setting.BalanceBlindBoxSmallPityThreshold - 1,
	})
	require.NoError(t, err)
	require.Equal(t, balanceBlindBoxGuaranteeSmall, small.Draws[0].GuaranteeType)

	big, err := SimulateBalanceBlindBoxes(100*unit, 1, BalanceBlindBoxSimulationState{
		PityProgress: setting.BalanceBlindBoxPityThreshold - 1,
	})
	require.NoError(t, err)
	require.Equal(t, balanceBlindBoxGuaranteeBig, big.Draws[0].GuaranteeType)
}
