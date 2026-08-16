package app

import (
	"math"
	"math/rand"
	"testing"

	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stretchr/testify/require"
)

func TestUnifiedBlindBoxPoolEconomics(t *testing.T) {
	setting := blindboxsettings.Get()
	require.Equal(t, 2.5, setting.BalanceBlindBoxPriceUSD)
	require.Len(t, setting.BalanceBlindBoxTiers, 12)
	require.InDelta(t, 0.30, setting.BalanceBlindBoxTiers[0].Probability, 0.000000001)

	var probability float64
	var expectedUnifiedCredit float64
	var immediateProfitProbability float64
	for _, tier := range setting.BalanceBlindBoxTiers {
		probability += tier.Probability
		if tier.RewardType == commerceschema.BlindBoxRewardTypeProp {
			continue
		}
		require.Equal(t, commerceschema.BlindBoxRewardTypeClaudeQuota, tier.RewardType)
		require.Equal(t, "claude", tier.WalletType)
		expectedUnifiedCredit += ((tier.MinUSD + tier.MaxUSD) / 2) * tier.Probability
		if tier.MinUSD >= setting.BalanceBlindBoxPriceUSD && tier.MaxUSD > setting.BalanceBlindBoxPriceUSD {
			immediateProfitProbability += tier.Probability
		}
	}
	require.InDelta(t, 1, probability, 0.000000001)
	require.InDelta(t, 2.63925, expectedUnifiedCredit, 0.000001)
	require.InDelta(t, 0.4163, immediateProfitProbability, 0.000001)
}

func TestUnifiedBlindBoxDrawUsesFrozenPoolAndGuarantees(t *testing.T) {
	setting := blindboxsettings.Get()
	setting.BalanceBlindBoxTiers = []blindboxsettings.TierSetting{
		{Name: "normal", MinUSD: 1, MaxUSD: 1, Probability: 1, RewardType: commerceschema.BlindBoxRewardTypeClaudeQuota},
	}
	setting.BalanceBlindBoxFirstDrawTiers = fixedGuaranteePool("first", 2.75)
	setting.BalanceBlindBoxSmallPityTiers = fixedGuaranteePool("small", 3)
	setting.BalanceBlindBoxPityTiers = fixedGuaranteePool("big", 9)

	first := drawBalanceBlindBoxReward(1, 1, 1, setting, &commerceschema.BalanceBlindBoxPityState{}, true)
	require.Equal(t, balanceBlindBoxGuaranteeFirst, first.GuaranteeType)
	require.Equal(t, "first", first.RewardTier)
	require.InDelta(t, 2.75, first.RewardUSD, 0.001)
	require.GreaterOrEqual(t, balanceBlindBoxEquivalentValue(first.RewardType, first.RewardUSD), setting.BalanceBlindBoxFirstDrawGuaranteeUSD)

	smallPity := commerceschema.BalanceBlindBoxPityState{ConsecutiveUnder6USD: setting.BalanceBlindBoxSmallPityThreshold - 1}
	small := drawBalanceBlindBoxReward(1, 1, 1, setting, &smallPity, false)
	require.Equal(t, balanceBlindBoxGuaranteeSmall, small.GuaranteeType)
	require.Equal(t, "small", small.RewardTier)
	require.GreaterOrEqual(t, balanceBlindBoxEquivalentValue(small.RewardType, small.RewardUSD), setting.BalanceBlindBoxSmallPityGuaranteeUSD)

	bigPity := commerceschema.BalanceBlindBoxPityState{ConsecutiveUnder35USD: setting.BalanceBlindBoxPityThreshold - 1}
	big := drawBalanceBlindBoxReward(1, 1, 1, setting, &bigPity, false)
	require.Equal(t, balanceBlindBoxGuaranteeBig, big.GuaranteeType)
	require.Equal(t, "big", big.RewardTier)
	require.GreaterOrEqual(t, balanceBlindBoxEquivalentValue(big.RewardType, big.RewardUSD), setting.BalanceBlindBoxPityGuaranteeUSD)
	require.Equal(t, balanceBlindBoxPoolVersion, big.PoolVersion)
	require.Equal(t, string(commerceschema.BlindBoxRewardWalletTypeClaude), big.RewardWalletType)
}

func TestUnifiedBlindBoxPityTriggersWithinConfiguredDrawCount(t *testing.T) {
	setting := blindboxsettings.Get()

	guaranteeType, _ := resolveBalanceBlindBoxGuarantee(&commerceschema.BalanceBlindBoxPityState{
		ConsecutiveUnder6USD: setting.BalanceBlindBoxSmallPityThreshold - 2,
	}, false, setting)
	require.Equal(t, balanceBlindBoxGuaranteeNone, guaranteeType)

	guaranteeType, _ = resolveBalanceBlindBoxGuarantee(&commerceschema.BalanceBlindBoxPityState{
		ConsecutiveUnder6USD: setting.BalanceBlindBoxSmallPityThreshold - 1,
	}, false, setting)
	require.Equal(t, balanceBlindBoxGuaranteeSmall, guaranteeType)

	guaranteeType, _ = resolveBalanceBlindBoxGuarantee(&commerceschema.BalanceBlindBoxPityState{
		ConsecutiveUnder35USD: setting.BalanceBlindBoxPityThreshold - 1,
	}, false, setting)
	require.Equal(t, balanceBlindBoxGuaranteeBig, guaranteeType)
}

func TestUnifiedBlindBoxMillionDrawEconomics(t *testing.T) {
	const draws = 1_000_000
	setting := blindboxsettings.Get()
	rng := rand.New(rand.NewSource(20260816))
	pity := commerceschema.BalanceBlindBoxPityState{}
	stats := blindBoxSimulationStats{}

	for range draws {
		var outcome float64
		pendingDraws := 1
		for pendingDraws > 0 {
			pendingDraws--
			reward, guaranteeType, extraDraw := simulateBalanceBlindBoxDraw(setting, &pity, rng)
			outcome += reward
			stats.countGuarantee(guaranteeType)
			if extraDraw {
				pendingDraws++
			}
		}
		stats.addOutcome(outcome)
	}

	mean, standardDeviation := stats.result(draws)
	t.Logf(
		"draws=%d mean=%.6f standard_deviation=%.6f small_pity_rate=%.6f big_pity_rate=%.6f",
		draws,
		mean,
		standardDeviation,
		float64(stats.smallPityCount)/draws,
		float64(stats.bigPityCount)/draws,
	)
	require.InDelta(t, 2.70, mean, 0.04)
	require.Greater(t, standardDeviation, 12.0)
	require.InDelta(t, 0.0031, float64(stats.smallPityCount)/draws, 0.002)
	require.InDelta(t, 0.0075, float64(stats.bigPityCount)/draws, 0.003)
}

type blindBoxSimulationStats struct {
	sum, sumSquares              float64
	smallPityCount, bigPityCount int
}

func (stats *blindBoxSimulationStats) addOutcome(reward float64) {
	stats.sum += reward
	stats.sumSquares += reward * reward
}

func (stats *blindBoxSimulationStats) countGuarantee(guaranteeType string) {
	if guaranteeType == balanceBlindBoxGuaranteeSmall {
		stats.smallPityCount++
	}
	if guaranteeType == balanceBlindBoxGuaranteeBig {
		stats.bigPityCount++
	}
}

func (stats blindBoxSimulationStats) result(draws int) (float64, float64) {
	mean := stats.sum / float64(draws)
	variance := stats.sumSquares/float64(draws) - mean*mean
	return mean, math.Sqrt(variance)
}

func simulateBalanceBlindBoxDraw(setting blindboxsettings.Setting, pity *commerceschema.BalanceBlindBoxPityState, rng *rand.Rand) (float64, string, bool) {
	guaranteeType, guaranteeUSD := resolveBalanceBlindBoxGuarantee(pity, false, setting)
	tiers := setting.BalanceBlindBoxTiers
	if guaranteeType != balanceBlindBoxGuaranteeNone {
		tiers = balanceBlindBoxGuaranteeTiers(setting, guaranteeType)
	}
	tier := pickBlindBoxTierForRoll(tiers, rng.Float64())
	rewardType := blindboxsettings.NormalizeRewardType(tier.RewardType)
	reward := tier.MinUSD + rng.Float64()*(tier.MaxUSD-tier.MinUSD)
	reward = math.Round(reward*100) / 100
	reward = applyBalanceBlindBoxGuaranteeMinimum(reward, rewardType, guaranteeUSD)
	if rewardType == commerceschema.BlindBoxRewardTypeProp {
		reward = 0
	}
	advanceBalanceBlindBoxPity(pity, rewardType, reward, setting)
	return reward, guaranteeType, rewardType == commerceschema.BlindBoxRewardTypeProp && tier.Name == "再来一抽"
}

func fixedGuaranteePool(name string, reward float64) []blindboxsettings.TierSetting {
	return []blindboxsettings.TierSetting{{
		Name: name, MinUSD: reward, MaxUSD: reward, Probability: 1,
		RewardType: commerceschema.BlindBoxRewardTypeClaudeQuota, WalletType: "claude",
	}}
}
