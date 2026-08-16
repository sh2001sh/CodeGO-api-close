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
	guaranteeType, guaranteeUSD := resolveBalanceBlindBoxGuarantee(pity, first, setting)
	tiers := setting.BalanceBlindBoxTiers
	if guaranteeUSD > 0 {
		tiers = balanceBlindBoxGuaranteeTiers(setting, guaranteeType)
	}
	tier := pickBlindBoxTier(tiers)
	rewardUSD := randomTierRewardUSD(tier)
	rewardType := blindboxsettings.NormalizeRewardType(tier.RewardType)
	walletType := commerceschema.BlindBoxRewardWalletTypeClaude
	if guaranteeUSD > 0 {
		rewardUSD = applyBalanceBlindBoxGuaranteeMinimum(rewardUSD, rewardType, guaranteeUSD)
	}

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
	if pity != nil {
		advanceBalanceBlindBoxPity(pity, item.RewardType, item.RewardUSD, setting)
	}
	return item
}

func resolveBalanceBlindBoxGuarantee(pity *commerceschema.BalanceBlindBoxPityState, first bool, setting blindboxsettings.Setting) (string, float64) {
	if pity != nil && balanceBlindBoxPityReady(pity.ConsecutiveUnder35USD, setting.BalanceBlindBoxPityThreshold) {
		return balanceBlindBoxGuaranteeBig, setting.BalanceBlindBoxPityGuaranteeUSD
	}
	if pity != nil && balanceBlindBoxPityReady(pity.ConsecutiveUnder6USD, setting.BalanceBlindBoxSmallPityThreshold) {
		return balanceBlindBoxGuaranteeSmall, setting.BalanceBlindBoxSmallPityGuaranteeUSD
	}
	if first && setting.BalanceBlindBoxFirstDrawGuaranteeUSD > 0 {
		return balanceBlindBoxGuaranteeFirst, setting.BalanceBlindBoxFirstDrawGuaranteeUSD
	}
	return balanceBlindBoxGuaranteeNone, 0
}

func balanceBlindBoxPityReady(progress, threshold int) bool {
	return threshold > 0 && progress >= threshold-1
}

func balanceBlindBoxGuaranteeTiers(setting blindboxsettings.Setting, guaranteeType string) []blindboxsettings.TierSetting {
	switch guaranteeType {
	case balanceBlindBoxGuaranteeFirst:
		return setting.BalanceBlindBoxFirstDrawTiers
	case balanceBlindBoxGuaranteeSmall:
		return setting.BalanceBlindBoxSmallPityTiers
	case balanceBlindBoxGuaranteeBig:
		return setting.BalanceBlindBoxPityTiers
	default:
		return setting.BalanceBlindBoxTiers
	}
}

func applyBalanceBlindBoxGuaranteeMinimum(rewardUSD float64, rewardType string, guaranteeUSD float64) float64 {
	if guaranteeUSD <= 0 || rewardType == commerceschema.BlindBoxRewardTypeProp {
		return rewardUSD
	}
	multiplier := balanceBlindBoxEquivalentValue(rewardType, 1)
	if multiplier <= 0 {
		return rewardUSD
	}
	minimum := guaranteeUSD / multiplier
	if rewardUSD < minimum {
		return minimum
	}
	return rewardUSD
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
