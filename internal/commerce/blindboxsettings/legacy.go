package blindboxsettings

import (
	"math"
	"strings"
)

func isApproxProbability(left, right float64) bool {
	return math.Abs(left-right) < 0.0001
}

func isLegacyBalanceBlindBoxTiers(tiers []TierSetting) bool {
	if len(tiers) == len(defaultBalanceBlindBoxTiers) &&
		(strings.TrimSpace(tiers[3].Name) == "2.50-3.9124 统一额度" || tiers[9].MaxUSD > 500) {
		return true
	}
	for _, probabilities := range legacyBalanceBlindBoxProbabilities {
		if len(tiers) != len(probabilities) {
			continue
		}
		matched := true
		for index, tier := range tiers {
			if !isApproxProbability(tier.Probability, probabilities[index]) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func isLegacyBrokenTiers(tiers []TierSetting) bool {
	for _, legacy := range legacyBrokenTierGroups() {
		if legacyTierGroupMatches(tiers, legacy) {
			return true
		}
	}
	return false
}

func legacyTierGroupMatches(tiers, legacy []TierSetting) bool {
	if len(tiers) != len(legacy) {
		return false
	}
	for index, tier := range tiers {
		target := legacy[index]
		if strings.TrimSpace(tier.Name) != target.Name ||
			!isApproxProbability(tier.MinUSD, target.MinUSD) ||
			!isApproxProbability(tier.MaxUSD, target.MaxUSD) ||
			!isApproxProbability(tier.Probability, target.Probability) {
			return false
		}
	}
	return true
}

func legacyBrokenTierGroups() [][]TierSetting {
	return [][]TierSetting{
		{
			{Name: "5 美元普通额度", MinUSD: 5, MaxUSD: 5, Probability: 0.10},
			{Name: "8 美元普通额度", MinUSD: 8, MaxUSD: 8, Probability: 0.16},
			{Name: "12 美元普通额度", MinUSD: 12, MaxUSD: 12, Probability: 0.18},
			{Name: "20 美元 Claude 额度", MinUSD: 20, MaxUSD: 20, Probability: 0.20},
			{Name: "30 美元 Claude 额度", MinUSD: 30, MaxUSD: 30, Probability: 0.14},
			{Name: "充值九折卡", Probability: 0.08},
			{Name: "套餐九折卡", Probability: 0.07},
			{Name: "0.95 倍率卡", Probability: 0.04},
			{Name: "0.9 倍率卡", Probability: 0.03},
			{Name: "免费调用次数卡（10 次）", Probability: 0.02},
		},
		{
			{Name: "5 美元普通额度", MinUSD: 5, MaxUSD: 5, Probability: 0.05},
			{Name: "8 美元普通额度", MinUSD: 8, MaxUSD: 8, Probability: 0.09},
			{Name: "12 美元普通额度", MinUSD: 12, MaxUSD: 12, Probability: 0.167},
			{Name: "20 美元 Claude 额度", MinUSD: 20, MaxUSD: 20, Probability: 0.23},
			{Name: "30 美元 Claude 额度", MinUSD: 30, MaxUSD: 30, Probability: 0.17},
			{Name: "充值九折卡", Probability: 0.08},
			{Name: "套餐九折卡", Probability: 0.07},
			{Name: "0.95 倍率卡", Probability: 0.05},
			{Name: "0.9 倍率卡", Probability: 0.04},
			{Name: "免费调用次数卡（10 次）", Probability: 0.05},
		},
	}
}
