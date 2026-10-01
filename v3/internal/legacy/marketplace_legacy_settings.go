package legacy

import (
	"encoding/json"
	"math/big"
	"strings"
)

// v2 Get() replaces its recognized historical distributions before applying
// the current pool. Reading stale option JSON directly would revive those odds.
func marketplaceEffectivePoolOptions(options map[string]string) {
	key := func(name string) string { return "blind_box_setting." + name }
	standardRaw, ok := options[key("tiers")]
	if !ok {
		standardRaw = marketplaceBalanceTiers
	}
	var standard []marketplaceTier
	if json.Unmarshal([]byte(standardRaw), &standard) == nil && len(standard) == 0 {
		standardRaw = marketplaceBalanceTiers
	}
	if marketplaceLegacyStandard(standard) {
		standardRaw = marketplaceBalanceTiers
		title := strings.TrimSpace(options[key("subscription_plan_title")])
		if title == "" || title == "Standard月卡" {
			options[key("subscription_plan_title")] = "Lite月卡"
		}
		probability := options[key("subscription_prize_probability")]
		if n := marketplaceSettingRat(probability); n != nil && (n.Sign() <= 0 || marketplaceSettingNear(probability, "0.001")) {
			options[key("subscription_prize_probability")] = "0.003"
		}
	}
	balanceRaw, ok := options[key("balance_blind_box_tiers")]
	if !ok {
		// Missing option keeps the initialized current default. An explicitly
		// empty array instead falls back to effective standard tiers in v2 Get().
		balanceRaw = marketplaceBalanceTiers
	}
	var balance []marketplaceTier
	if json.Unmarshal([]byte(balanceRaw), &balance) != nil {
		return // tierRewards reports the malformed value; never silently fix it.
	}
	if len(balance) == 0 {
		balanceRaw = standardRaw
		if json.Unmarshal([]byte(balanceRaw), &balance) != nil {
			options[key("balance_blind_box_tiers")] = balanceRaw
			return
		}
	}
	if marketplaceLegacyBalance(balance) {
		var effective []marketplaceTier
		if json.Unmarshal([]byte(standardRaw), &effective) == nil && !marketplaceLegacyBalance(effective) {
			balanceRaw = standardRaw
		} else {
			balanceRaw = marketplaceBalanceTiers
		}
	}
	options[key("balance_blind_box_tiers")] = balanceRaw
}

func marketplaceSettingRat(raw string) *big.Rat {
	if raw == "" {
		raw = "0"
	}
	n, _ := new(big.Rat).SetString(raw)
	return n
}

func marketplaceSettingNear(left, right string) bool {
	l, r := marketplaceSettingRat(left), marketplaceSettingRat(right)
	if l == nil || r == nil {
		return false
	}
	return new(big.Rat).Abs(new(big.Rat).Sub(l, r)).Cmp(big.NewRat(1, 10000)) < 0
}

func marketplaceLegacyStandard(tiers []marketplaceTier) bool {
	names := []string{"5 美元普通额度", "8 美元普通额度", "12 美元普通额度", "20 美元 Claude 额度", "30 美元 Claude 额度", "充值九折卡", "套餐九折卡", "0.95 倍率卡", "0.9 倍率卡", "免费调用次数卡（10 次）"}
	amounts := []string{"5", "8", "12", "20", "30", "0", "0", "0", "0", "0"}
	if len(tiers) != len(names) {
		return false
	}
	for i, tier := range tiers {
		if strings.TrimSpace(tier.Name) != names[i] || !marketplaceSettingNear(tier.Min.String(), amounts[i]) || !marketplaceSettingNear(tier.Max.String(), amounts[i]) {
			return false
		}
	}
	for _, pattern := range [][]string{{".10", ".16", ".18", ".20", ".14", ".08", ".07", ".04", ".03", ".02"}, {".05", ".09", ".167", ".23", ".17", ".08", ".07", ".05", ".04", ".05"}} {
		if marketplaceProbabilityPattern(tiers, pattern) {
			return true
		}
	}
	return false
}

func marketplaceLegacyBalance(tiers []marketplaceTier) bool {
	if len(tiers) == 12 {
		max := marketplaceSettingRat(tiers[9].Max.String())
		if strings.TrimSpace(tiers[3].Name) == "2.50-3.9124 统一额度" || max != nil && max.Cmp(big.NewRat(500, 1)) > 0 {
			return true
		}
	}
	// Exact decimal fingerprints from v2 blindboxsettings/pools.go.
	patterns := [][]string{
		{".29311", ".13", ".08", ".429317727273", ".045", ".008", ".0007", ".00015", ".00002", ".000002272727", ".0127", ".001"},
		{".30", ".17", ".10", ".35", ".055", ".01", ".001", ".00025", ".00004", ".00001", ".0127", ".001"},
		{".2066", ".24", ".0975", ".2759", ".02", ".012", ".006", ".0015", ".0004", ".0001", ".06", ".04", ".01", ".03"},
		{".4366", ".24", ".0975", ".045", ".02", ".012", ".006", ".002", ".0007", ".0002", ".06", ".04", ".01", ".03"},
		{".1537", ".27", ".25", ".12", ".05", ".01115", ".004", ".0008", ".0003", ".00005", ".06", ".04", ".01", ".03"},
		{".12", ".17", ".10", ".075", ".19", ".025", ".004", ".00075", ".00036", ".00004", ".075", ".19", ".025", ".004", ".00075", ".007", ".004", ".006", ".0031"},
		{".35", ".18", ".10", ".10", ".18", ".04", ".025", ".006", ".0015", ".001", ".002", ".001", ".0005", ".0003", ".0001", ".0045", ".0025", ".0035", ".0021"},
		{".12", ".16", ".18", ".18", ".20127", ".03", ".006", ".001", ".0002", ".00003", ".04", ".035", ".02", ".008", ".002", ".0065", ".0035", ".0045", ".002"},
		{".08", ".12", ".16", ".20", ".25", ".03", ".0043", ".00058", ".0001", ".00002", ".04", ".035", ".02", ".008", ".002", ".0065", ".0035", ".0045", ".002"},
		{".17", ".145", ".1719", ".20", ".20", ".03", ".0043", ".00058", ".0001", ".00002", ".015", ".008", ".004", ".001", ".0001", ".0065", ".0035", ".0045", ".002"},
		{".45197", ".18", ".09", ".07", ".05", ".075", ".025", ".00625", ".0015", ".00088", ".002", ".001", ".0005", ".0003", ".0001", ".0065", ".0035", ".0045", ".002"},
		{".45057", ".18", ".09", ".07", ".05", ".075", ".025", ".00675", ".002", ".00128", ".002", ".001", ".0005", ".0003", ".0001", ".0065", ".0035", ".0045", ".002"},
	}
	for _, pattern := range patterns {
		if marketplaceProbabilityPattern(tiers, pattern) {
			return true
		}
	}
	return false
}

func marketplaceProbabilityPattern(tiers []marketplaceTier, pattern []string) bool {
	if len(tiers) != len(pattern) {
		return false
	}
	for i, tier := range tiers {
		if !marketplaceSettingNear(tier.Probability.String(), pattern[i]) {
			return false
		}
	}
	return true
}
