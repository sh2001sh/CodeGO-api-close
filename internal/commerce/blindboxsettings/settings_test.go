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
	require.InDelta(t, 0.22, setting.Tiers[0].Probability, 0.000000001)
	require.Equal(t, "500.00 统一额度", setting.Tiers[9].Name)
	require.Equal(t, "再来一抽", setting.Tiers[10].Name)
	require.Equal(t, "15 分钟 0.1 倍率卡", setting.Tiers[11].Name)
	require.Equal(t, 10, setting.DailyLimit)
	require.Equal(t, 10, setting.BalanceBlindBoxDailyPurchaseLimit)
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
	legacyProbabilities := legacyBalanceBlindBoxProbabilities[0]
	legacy := make([]TierSetting, len(legacyProbabilities))
	for index := range legacy {
		legacy[index].Probability = legacyProbabilities[index]
	}
	setting.Tiers = legacy
	setting.BalanceBlindBoxTiers = copyTierSettings(legacy)
	Set(setting)

	normalized := Get()
	require.InDelta(t, 0.22, normalized.BalanceBlindBoxTiers[0].Probability, 0.000000001)
	require.InDelta(t, 0.000002272727, normalized.BalanceBlindBoxTiers[9].Probability, 0.000000001)
}

func TestGetPreservesConfiguredUnifiedBlindBoxPrice(t *testing.T) {
	original := Get()
	t.Cleanup(func() { Set(original) })

	setting := original
	setting.BalanceBlindBoxPriceUSD = 3.25
	Set(setting)

	require.Equal(t, 3.25, Get().BalanceBlindBoxPriceUSD)
}

func TestGetMigratesPreviousCappedPoolRange(t *testing.T) {
	original := Get()
	t.Cleanup(func() { Set(original) })

	setting := original
	previous := copyTierSettings(defaultBalanceBlindBoxTiers)
	previous[3].Name = "2.50-3.9124 统一额度"
	previous[3].MaxUSD = 3.9124
	setting.Tiers = copyTierSettings(previous)
	setting.BalanceBlindBoxTiers = previous
	Set(setting)

	normalized := Get()
	require.Equal(t, "2.50-3.69 统一额度", normalized.BalanceBlindBoxTiers[3].Name)
	require.Equal(t, 3.69, normalized.BalanceBlindBoxTiers[3].MaxUSD)
	require.LessOrEqual(t, normalized.BalanceBlindBoxTiers[9].MaxUSD, 500.0)
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

func TestGetPreservesConfiguredUnifiedBlindBoxDailyLimit(t *testing.T) {
	original := Get()
	t.Cleanup(func() { Set(original) })

	setting := original
	setting.DailyLimit = 500
	setting.BalanceBlindBoxDailyPurchaseLimit = 500
	setting.CountOptions = []int{1, 5, 10, 20, 50}
	Set(setting)

	normalized := Get()
	require.Equal(t, 10, normalized.DailyLimit)
	require.Equal(t, 500, normalized.BalanceBlindBoxDailyPurchaseLimit)
	require.Equal(t, []int{1, 5, 10}, normalized.CountOptions)
}
