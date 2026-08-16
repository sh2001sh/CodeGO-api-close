package app

import (
	"testing"

	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stretchr/testify/require"
)

func TestUnifiedBlindBoxPoolEconomics(t *testing.T) {
	setting := blindboxsettings.Get()
	require.Equal(t, 2.5, setting.BalanceBlindBoxPriceUSD)
	require.Len(t, setting.BalanceBlindBoxTiers, 14)
	require.InDelta(t, 0.1537, setting.BalanceBlindBoxTiers[0].Probability, 0.000000001)

	var probability float64
	var expectedUnifiedCredit float64
	for _, tier := range setting.BalanceBlindBoxTiers {
		probability += tier.Probability
		if tier.RewardType == commerceschema.BlindBoxRewardTypeProp {
			continue
		}
		require.Equal(t, commerceschema.BlindBoxRewardTypeClaudeQuota, tier.RewardType)
		require.Equal(t, "claude", tier.WalletType)
		expectedUnifiedCredit += ((tier.MinUSD + tier.MaxUSD) / 2) * tier.Probability
	}
	require.InDelta(t, 1, probability, 0.000000001)
	require.InDelta(t, 2.1673, expectedUnifiedCredit, 0.0001)
}

func TestUnifiedBlindBoxDrawUsesFrozenPoolAndNoPityOverride(t *testing.T) {
	setting := blindboxsettings.Get()
	pity := commerceschema.BalanceBlindBoxPityState{ConsecutiveUnder6USD: 999, ConsecutiveUnder35USD: 999}
	for index := 0; index < 2_000; index++ {
		item := issueSealedBalanceBlindBox(1, 1, setting, &pity, index == 0)
		require.Equal(t, balanceBlindBoxPoolVersion, item.PoolVersion)
		require.False(t, item.IsPity)
		if item.RewardType != commerceschema.BlindBoxRewardTypeProp {
			require.Equal(t, commerceschema.BlindBoxRewardTypeClaudeQuota, item.RewardType)
			require.Equal(t, string(commerceschema.BlindBoxRewardWalletTypeClaude), item.RewardWalletType)
		}
	}
}
