package app

import (
	"fmt"

	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
)

const (
	balanceBlindBoxGuaranteeNone  = "none"
	balanceBlindBoxGuaranteeFirst = "first"
	balanceBlindBoxGuaranteeSmall = "small"
	balanceBlindBoxGuaranteeBig   = "big"
)

func issueSealedBalanceBlindBox(purchaseID, purchaserID int, setting blindboxsettings.Setting, pity *commerceschema.BalanceBlindBoxPityState, first bool) commerceschema.BalanceBlindBoxItem {
	tier := pickBlindBoxTier(setting.BalanceBlindBoxTiers)
	rewardUSD := randomTierRewardUSD(tier)
	rewardType := blindboxsettings.NormalizeRewardType(tier.RewardType)
	walletType := commerceschema.BlindBoxRewardWalletTypeClaude
	guaranteeType := balanceBlindBoxGuaranteeNone
	_ = pity
	_ = first

	item := commerceschema.BalanceBlindBoxItem{
		PurchaseId: purchaseID, PurchaseUserId: purchaserID, OwnerUserId: purchaserID, PoolVersion: balanceBlindBoxPoolVersion,
		RewardType: rewardType, RewardTier: tier.Name, RewardUSD: rewardUSD, RewardWalletType: string(walletType),
		IsPity: guaranteeType != balanceBlindBoxGuaranteeNone, GuaranteeType: guaranteeType,
	}
	if rewardType == commerceschema.BlindBoxRewardTypeProp {
		item.RewardTitle = tier.Name
		item.RewardWalletType = ""
		item.RewardUSD = 0
	} else {
		rewardType = commerceschema.BlindBoxRewardTypeClaudeQuota
		item.CreditAmount = quotaUnitsFromBlindBoxUSD(rewardUSD)
		item.RewardType = rewardType
		item.RewardTitle = fmt.Sprintf("%.2f 统一额度奖励", rewardUSD)
	}
	return item
}

func advanceBalanceBlindBoxPity(pity *commerceschema.BalanceBlindBoxPityState, rewardType string, rewardUSD float64, setting blindboxsettings.Setting) {
	rewardValue := balanceBlindBoxEquivalentValue(rewardType, rewardUSD)
	if rewardValue >= setting.BalanceBlindBoxPityGuaranteeUSD {
		pity.ConsecutiveUnder6USD, pity.ConsecutiveUnder35USD = 0, 0
		return
	}
	if rewardValue >= setting.BalanceBlindBoxSmallPityGuaranteeUSD {
		pity.ConsecutiveUnder6USD = 0
		pity.ConsecutiveUnder35USD++
		return
	}
	pity.ConsecutiveUnder6USD++
	pity.ConsecutiveUnder35USD++
}

func balanceBlindBoxEquivalentValue(rewardType string, rewardUSD float64) float64 {
	if rewardType == commerceschema.BlindBoxRewardTypeClaudeQuota {
		return rewardUSD * 4
	}
	if rewardType == commerceschema.BlindBoxRewardTypeProp {
		return 0
	}
	return rewardUSD
}
