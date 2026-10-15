package app

import (
	"math/rand"
	"sort"
	"testing"

	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
)

func TestSimulatedBlindBoxHundredDollarReinvestment(t *testing.T) {
	const (
		sessions        = 1_000
		targetPaidDraws = 1_800
		batchSize       = 10
		startingBalance = 100.0
	)
	setBalanceBlindBoxTestSetting(t, 100)
	unit := int64(platformruntime.QuotaPerUnit)
	finalBalances := make([]float64, sessions)
	paidDrawCounts := make([]int, sessions)
	reachedTarget, profitable := 0, 0
	for session := range sessions {
		finalBalances[session], paidDrawCounts[session] = simulateReinvestmentThroughPublicSimulator(
			int64(startingBalance*float64(unit)), targetPaidDraws, batchSize,
		)
		if paidDrawCounts[session] == targetPaidDraws {
			reachedTarget++
		}
		if finalBalances[session] >= startingBalance {
			profitable++
		}
	}

	sort.Float64s(finalBalances)
	sort.Ints(paidDrawCounts)
	t.Logf(
		"sessions=%d target_draws=%d reached_target_rate=%.4f profitable_rate=%.4f median_draws=%d median_balance=%.2f p90_balance=%.2f",
		sessions, targetPaidDraws, float64(reachedTarget)/sessions, float64(profitable)/sessions,
		paidDrawCounts[sessions/2], finalBalances[sessions/2], finalBalances[sessions*9/10],
	)
	require.Greater(t, float64(reachedTarget)/sessions, 0.50)
	require.Greater(t, float64(profitable)/sessions, 0.50)
	require.Greater(t, paidDrawCounts[sessions/2], targetPaidDraws-1)
	require.Greater(t, finalBalances[sessions/2], startingBalance)
}

func TestUnifiedBlindBoxEighteenHundredPurchaseDistribution(t *testing.T) {
	const (
		sessions        = 10_000
		paidDraws       = 1_800
		startingBalance = 100.0
	)
	setting := blindboxsettings.Get()
	rng := rand.New(rand.NewSource(20260819))
	finalBalances := make([]float64, sessions)
	profitable := 0
	for session := range sessions {
		reward := simulateBlindBoxPaidDraws(setting, paidDraws, rng)
		finalBalances[session] = startingBalance - float64(paidDraws)*setting.BalanceBlindBoxPriceUSD + reward
		if finalBalances[session] >= startingBalance {
			profitable++
		}
	}

	sort.Float64s(finalBalances)
	profitRate := float64(profitable) / sessions
	median := finalBalances[sessions/2]
	p90 := finalBalances[sessions*9/10]
	p99 := finalBalances[sessions*99/100]
	t.Logf("sessions=%d paid_draws=%d profitable_rate=%.6f median_balance=%.2f p90=%.2f p99=%.2f", sessions, paidDraws, profitRate, median, p90, p99)
	require.Greater(t, profitRate, 0.50)
	require.Greater(t, median, startingBalance)
	require.Less(t, p90, 300.0)
	require.Less(t, p99, 500.0)
}

func simulateBlindBoxPaidDraws(setting blindboxsettings.Setting, paidDraws int, rng *rand.Rand) float64 {
	pity := commerceschema.BalanceBlindBoxPityState{}
	firstDraw := true
	var reward float64
	for range paidDraws {
		pendingDraws := 1
		for pendingDraws > 0 {
			pendingDraws--
			value, _, extraDraw := simulateBalanceBlindBoxDraw(setting, &pity, rng, firstDraw)
			firstDraw = false
			reward += value
			if extraDraw {
				pendingDraws++
			}
		}
	}
	return reward
}

func simulateReinvestmentThroughPublicSimulator(balance int64, targetPaidDraws, batchSize int) (float64, int) {
	unit := int64(platformruntime.QuotaPerUnit)
	price := quotaUnitsFromBlindBoxUSD(blindboxsettings.Get().BalanceBlindBoxPriceUSD)
	state := BalanceBlindBoxSimulationState{FirstDrawEligible: true}
	paidDraws := 0
	for paidDraws < targetPaidDraws && balance >= price {
		count := min(batchSize, targetPaidDraws-paidDraws, int(balance/price))
		result, err := SimulateBalanceBlindBoxes(balance, count, state)
		if err != nil {
			return float64(balance) / float64(unit), paidDraws
		}
		balance = result.BalanceAfter
		paidDraws += count
		state = BalanceBlindBoxSimulationState{
			SmallPityProgress: result.SmallPityProgress,
			PityProgress:      result.PityProgress,
			FirstDrawEligible: result.FirstDrawEligible,
		}
	}
	return float64(balance) / float64(unit), paidDraws
}
