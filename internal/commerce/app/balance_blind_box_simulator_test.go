package app

import (
	"testing"

	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
)

func TestSimulateBalanceBlindBoxesUsesUnifiedPoolWithoutNegativeBalance(t *testing.T) {
	setBalanceBlindBoxTestSetting(t, 100)
	unit := int64(platformruntime.QuotaPerUnit)
	result, err := SimulateBalanceBlindBoxes(100*unit, 10)
	require.NoError(t, err)
	require.Len(t, result.Draws, 10)
	require.Equal(t, result.BalanceBefore-result.CostQuota+result.RewardQuota, result.BalanceAfter)
	require.GreaterOrEqual(t, result.BalanceAfter, int64(0))
	for _, draw := range result.Draws {
		require.NotEmpty(t, draw.RewardType)
		require.NotEmpty(t, draw.RewardTier)
		require.NotEmpty(t, draw.RewardTitle)
	}
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
