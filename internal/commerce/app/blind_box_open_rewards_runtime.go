package app

import (
	"errors"
	"fmt"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	"math"
	"math/rand"

	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	"gorm.io/gorm"
)

func formatFirstPurchaseBlindBoxRewardTitle(amount float64) string {
	return fmt.Sprintf("首购专属奖励：%.2f 美元", amount)
}

func blindBoxWalletLogLabel(walletType commerceschema.BlindBoxRewardWalletType) string {
	if walletType == commerceschema.BlindBoxRewardWalletTypeClaude {
		return "Claude额度"
	}
	return "额度"
}

func recordBlindBoxRewardLogTx(tx *gorm.DB, userID int, amount int64, walletType commerceschema.BlindBoxRewardWalletType, record *commerceschema.BlindBoxOpenRecord) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if userID <= 0 || amount <= 0 || record == nil {
		return errors.New("invalid blind box reward log params")
	}
	content := fmt.Sprintf(
		"盲盒开奖到账，钱包：%s，到账额度：%s，奖励：%s，开奖记录ID：%d",
		blindBoxWalletLogLabel(walletType),
		logger.LogQuota(int(amount)),
		record.RewardTitle,
		record.Id,
	)
	return auditapp.RecordLogTx(tx, userID, auditschema.LogTypeTopup, content)
}

func quotaUnitsFromBlindBoxUSD(amount float64) int64 {
	if amount <= 0 {
		return 0
	}
	return quotaUnitsFromUSD(amount)
}

func normalizeBlindBoxRewardWalletType(value string) commerceschema.BlindBoxRewardWalletType {
	if value == string(commerceschema.BlindBoxRewardWalletTypeClaude) {
		return commerceschema.BlindBoxRewardWalletTypeClaude
	}
	return commerceschema.BlindBoxRewardWalletTypeDefault
}

func creditBlindBoxRewardByWalletTx(tx *gorm.DB, userID int, amount int64, walletType commerceschema.BlindBoxRewardWalletType, idempotencyKey string, reasonCode string) error {
	if amount <= 0 {
		return errors.New("invalid blind box reward amount")
	}
	if idempotencyKey == "" {
		return errors.New("blind box reward idempotency key is required")
	}
	switch walletType {
	case commerceschema.BlindBoxRewardWalletTypeClaude:
		return billingapp.CreditClaudeWalletQuotaTx(tx, userID, int(amount), idempotencyKey, reasonCode)
	default:
		return billingapp.CreditWalletQuotaTx(tx, userID, int(amount), idempotencyKey, reasonCode)
	}
}

func applyBlindBoxWalletRewardTx(tx *gorm.DB, userID int, openRecordID int, amount int64, walletType commerceschema.BlindBoxRewardWalletType) error {
	idempotencyKey := fmt.Sprintf("blind-box:reward:%d:%s", openRecordID, walletType)
	return creditBlindBoxRewardByWalletTx(tx, userID, amount, walletType, idempotencyKey, "blind_box_reward")
}

func applyFirstPurchaseMinimumGuarantee(isFirstPurchaseOpen bool, ordinaryMinimumUSD float64, claudeMinimumUSD float64, rewardUSD *float64, rewardType *string, walletType *commerceschema.BlindBoxRewardWalletType) {
	if !isFirstPurchaseOpen || rewardUSD == nil || rewardType == nil || walletType == nil {
		return
	}
	switch *rewardType {
	case commerceschema.BlindBoxRewardTypeProp:
		if ordinaryMinimumUSD > 0 {
			*rewardUSD = ordinaryMinimumUSD
			*rewardType = commerceschema.BlindBoxRewardTypeQuota
			*walletType = commerceschema.BlindBoxRewardWalletTypeDefault
		}
	case commerceschema.BlindBoxRewardTypeQuota:
		if ordinaryMinimumUSD > 0 && *rewardUSD < ordinaryMinimumUSD {
			*rewardUSD = ordinaryMinimumUSD
			*walletType = commerceschema.BlindBoxRewardWalletTypeDefault
		}
	case commerceschema.BlindBoxRewardTypeClaudeQuota:
		if claudeMinimumUSD > 0 && *rewardUSD < claudeMinimumUSD {
			*rewardUSD = claudeMinimumUSD
			*walletType = commerceschema.BlindBoxRewardWalletTypeClaude
		}
	}
}

func pickBlindBoxTier(tiers []blindboxsettings.TierSetting) blindboxsettings.TierSetting {
	return pickBlindBoxTierForRoll(tiers, rand.Float64())
}

func pickBlindBoxTierForRoll(tiers []blindboxsettings.TierSetting, roll float64) blindboxsettings.TierSetting {
	if len(tiers) == 0 {
		return blindboxsettings.TierSetting{Name: "fallback", MinUSD: 1, MaxUSD: 1, Probability: 1}
	}
	totalProbability := 0.0
	lastEligible := -1
	for index, tier := range tiers {
		if tier.Probability <= 0 {
			continue
		}
		totalProbability += tier.Probability
		lastEligible = index
	}
	if lastEligible == -1 {
		return blindboxsettings.TierSetting{Name: "fallback", MinUSD: 1, MaxUSD: 1, Probability: 1}
	}
	if roll < 0 {
		roll = 0
	}
	if roll >= 1 {
		roll = math.Nextafter(1, 0)
	}
	roll *= totalProbability
	cumulative := 0.0
	for _, tier := range tiers {
		if tier.Probability <= 0 {
			continue
		}
		cumulative += tier.Probability
		if roll <= cumulative {
			return tier
		}
	}
	return tiers[lastEligible]
}

func randomTierRewardUSD(tier blindboxsettings.TierSetting) float64 {
	if tier.MaxUSD <= tier.MinUSD {
		return math.Round(tier.MinUSD*100) / 100
	}
	value := tier.MinUSD + rand.Float64()*(tier.MaxUSD-tier.MinUSD)
	return math.Round(value*100) / 100
}

func isBlindBoxHighValueReward(rewardType string, rewardUSD float64, thresholdUSD float64) bool {
	if rewardUSD <= 0 || thresholdUSD <= 0 {
		return false
	}
	valueEquivalent := rewardUSD
	if rewardType == commerceschema.BlindBoxRewardTypeClaudeQuota {
		valueEquivalent = rewardUSD * 10
	}
	return valueEquivalent >= thresholdUSD
}
