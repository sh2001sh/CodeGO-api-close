package blindboxsettings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetUsesUnifiedBlindBoxPoolForBothPaymentEntries(t *testing.T) {
	setting := Get()
	require.Equal(t, 2.5, setting.UnitPrice)
	require.Equal(t, 2.5, setting.BalanceBlindBoxPriceUSD)
	require.Equal(t, setting.Tiers, setting.BalanceBlindBoxTiers)
	require.InDelta(t, 0.2066, setting.Tiers[0].Probability, 0.000000001)
	require.Equal(t, "200.00-1000.00 统一额度", setting.Tiers[9].Name)
	require.Equal(t, "0.1 倍率卡", setting.Tiers[12].Name)
	require.Len(t, setting.BalanceBlindBoxFirstDrawTiers, 3)
	require.Len(t, setting.BalanceBlindBoxSmallPityTiers, 3)
	require.Len(t, setting.BalanceBlindBoxPityTiers, 3)
	require.InDelta(t, defaultSubscriptionPrizeProbability, setting.SubscriptionPrizeProbability, 0.000000001)
	require.Equal(t, defaultSubscriptionPlanTitle, setting.SubscriptionPlanTitle)
}

func TestGetMigratesKnownLegacyUnifiedPool(t *testing.T) {
	original := Get()
	t.Cleanup(func() { Set(original) })

	setting := original
	legacy := copyTierSettings(defaultTierSettings)
	legacyProbabilities := legacyBalanceBlindBoxProbabilities[0]
	for index := range legacy {
		legacy[index].Probability = legacyProbabilities[index]
	}
	setting.Tiers = legacy
	setting.BalanceBlindBoxTiers = copyTierSettings(legacy)
	Set(setting)

	normalized := Get()
	require.InDelta(t, 0.2066, normalized.BalanceBlindBoxTiers[0].Probability, 0.000000001)
	require.InDelta(t, 0.0001, normalized.BalanceBlindBoxTiers[9].Probability, 0.000000001)
}

func TestGetNormalizesLegacyQuotaRewardsToUnifiedCredit(t *testing.T) {
	original := Get()
	t.Cleanup(func() { Set(original) })

	setting := original
	setting.Tiers = []TierSetting{
		{Name: "legacy quota", MinUSD: 1, MaxUSD: 1, Probability: 0.5, RewardType: "quota", WalletType: "default"},
		{Name: "legacy claude quota", MinUSD: 2, MaxUSD: 2, Probability: 0.5, RewardType: "claude_quota", WalletType: "claude"},
	}
	setting.BalanceBlindBoxTiers = copyTierSettings(setting.Tiers)
	Set(setting)

	normalized := Get()
	require.Len(t, normalized.Tiers, 2)
	for _, tier := range normalized.Tiers {
		require.Equal(t, "claude_quota", tier.RewardType)
		require.Equal(t, "claude", tier.WalletType)
	}
}
