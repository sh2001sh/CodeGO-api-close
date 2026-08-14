package app

import (
	"math/rand"
	"testing"

	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stretchr/testify/require"
)

func TestBalanceBlindBoxPoolEconomics(t *testing.T) {
	setting := blindboxsettings.Get()
	require.LessOrEqual(t, setting.BalanceBlindBoxTiers[0].Probability, 0.12)
	for _, pair := range [][2]int{{3, 10}, {4, 11}, {5, 12}, {6, 13}, {7, 14}} {
		require.InDelta(
			t,
			setting.BalanceBlindBoxTiers[pair[0]].Probability,
			setting.BalanceBlindBoxTiers[pair[1]].Probability,
			0.000000001,
		)
	}
	var probability float64
	var atLeastCostProbability float64
	for _, tier := range setting.BalanceBlindBoxTiers {
		probability += tier.Probability
		if tierMinimumEquivalentValue(tier) >= setting.BalanceBlindBoxPriceUSD {
			atLeastCostProbability += tier.Probability
		}
	}
	require.InDelta(t, 1, probability, 0.000000001)
	require.Greater(t, atLeastCostProbability, 0.43)

	rand.Seed(20260814)
	const draws = 1_000_000
	pity := commerceschema.BalanceBlindBoxPityState{}
	var totalValue float64
	var atLeast10, atLeast15 int
	for index := 0; index < draws; index++ {
		item := issueSealedBalanceBlindBox(1, 1, setting, &pity, index == 0)
		value := balanceBlindBoxEconomicValue(item)
		totalValue += value
		if value >= 10 {
			atLeast10++
		}
		if value >= setting.BalanceBlindBoxPriceUSD {
			atLeast15++
		}
	}
	average := totalValue / draws
	require.Less(t, average, setting.BalanceBlindBoxPriceUSD)
	require.Greater(t, float64(atLeast10)/draws, 0.58)
	require.Greater(t, float64(atLeast15)/draws, 0.43)
	t.Logf("draws=%d average=$%.4f rtp=%.2f%% >=$10=%.2f%% >=$15=%.2f%%", draws, average, average/setting.BalanceBlindBoxPriceUSD*100, float64(atLeast10)/draws*100, float64(atLeast15)/draws*100)

	for _, batch := range []int{10, 20, 50, 100, 1000} {
		trials := 20_000
		if batch == 1000 {
			trials = 5_000
		}
		profitable := 0
		for trial := 0; trial < trials; trial++ {
			batchPity := commerceschema.BalanceBlindBoxPityState{}
			var value float64
			for index := 0; index < batch; index++ {
				item := issueSealedBalanceBlindBox(trial+1, trial+1, setting, &batchPity, index == 0)
				value += balanceBlindBoxEconomicValue(item)
			}
			if value > float64(batch)*setting.BalanceBlindBoxPriceUSD {
				profitable++
			}
		}
		t.Logf("batch=%d trials=%d profitable=%d probability=%.2f%%", batch, trials, profitable, float64(profitable)/float64(trials)*100)
	}
}

func tierMinimumEquivalentValue(tier blindboxsettings.TierSetting) float64 {
	return balanceBlindBoxEquivalentValue(
		blindboxsettings.NormalizeRewardType(tier.RewardType),
		tier.MinUSD,
	)
}

func balanceBlindBoxEconomicValue(item commerceschema.BalanceBlindBoxItem) float64 {
	return balanceBlindBoxEquivalentValue(item.RewardType, item.RewardUSD)
}
