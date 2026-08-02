package blindboxsettings

import (
	"math"
	"sort"
	"strings"

	"github.com/sh2001sh/new-api/setting/config"
)

// TierSetting defines a single reward tier for blind-box draws.
type TierSetting struct {
	Name        string  `json:"name"`
	MinUSD      float64 `json:"min_usd"`
	MaxUSD      float64 `json:"max_usd"`
	Probability float64 `json:"probability"`
	RewardType  string  `json:"reward_type,omitempty"`
	WalletType  string  `json:"wallet_type,omitempty"`
}

// Setting stores the runtime blind-box configuration.
type Setting struct {
	Enabled                      bool          `json:"enabled"`
	UnitPrice                    float64       `json:"unit_price"`
	ExpireDays                   int           `json:"expire_days"`
	DailyLimit                   int           `json:"daily_limit"`
	MonthlyLimit                 int           `json:"monthly_limit"`
	DailyOpenLimit               int           `json:"daily_open_limit"`
	FirstPurchaseGuaranteeUSD    float64       `json:"first_purchase_guarantee_usd"`
	PityThreshold                int           `json:"pity_threshold"`
	PityGuaranteeUSD             float64       `json:"pity_guarantee_usd"`
	LowRewardThresholdUSD        float64       `json:"low_reward_threshold_usd"`
	SubscriptionPrizeProbability float64       `json:"subscription_prize_probability"`
	SubscriptionPlanTitle        string        `json:"subscription_plan_title"`
	CountOptions                 []int         `json:"count_options"`
	Tiers                        []TierSetting `json:"tiers"`
}

const (
	defaultSubscriptionPrizeProbability = 0.003
	defaultSubscriptionPlanTitle        = "Lite月卡"
)

var defaultTierSettings = []TierSetting{
	// Value model:
	// - 1 Claude 额度 ≈ 1 RMB 成本
	// - 1 美元普通额度 ≈ 0.1 RMB 成本
	// Target:
	// - medium rewards carry the highest probability mass
	// - low / jackpot rewards stay small probability
	// - Claude rewards have enough presence and larger-span tiers
	// - total expected payout remains below the 2.5 RMB box price
	{Name: "2-5 美元普通额度", MinUSD: 2.0, MaxUSD: 5.0, Probability: 0.09, RewardType: "quota", WalletType: "default"},
	{Name: "5-10 美元普通额度", MinUSD: 5.0, MaxUSD: 10.0, Probability: 0.18, RewardType: "quota", WalletType: "default"},
	{Name: "10-20 美元普通额度", MinUSD: 10.0, MaxUSD: 20.0, Probability: 0.21, RewardType: "quota", WalletType: "default"},
	{Name: "20-30 美元普通额度", MinUSD: 20.0, MaxUSD: 30.0, Probability: 0.075, RewardType: "quota", WalletType: "default"},
	{Name: "30-50 美元普通额度", MinUSD: 30.0, MaxUSD: 50.0, Probability: 0.027, RewardType: "quota", WalletType: "default"},
	{Name: "50-80 美元普通额度", MinUSD: 50.0, MaxUSD: 80.0, Probability: 0.008, RewardType: "quota", WalletType: "default"},
	{Name: "80-120 美元普通额度", MinUSD: 80.0, MaxUSD: 120.0, Probability: 0.002, RewardType: "quota", WalletType: "default"},
	{Name: "0.5-1 Claude 额度", MinUSD: 0.5, MaxUSD: 1.0, Probability: 0.11, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "1-2 Claude 额度", MinUSD: 1.0, MaxUSD: 2.0, Probability: 0.09, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "2-5 Claude 额度", MinUSD: 2.0, MaxUSD: 5.0, Probability: 0.055, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "5-10 Claude 额度", MinUSD: 5.0, MaxUSD: 10.0, Probability: 0.03, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "10-20 Claude 额度", MinUSD: 10.0, MaxUSD: 20.0, Probability: 0.012, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "20-40 Claude 额度", MinUSD: 20.0, MaxUSD: 40.0, Probability: 0.006, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "40-60 Claude 额度", MinUSD: 40.0, MaxUSD: 60.0, Probability: 0.001, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "充值九折卡", MinUSD: 0, MaxUSD: 0, Probability: 0.028, RewardType: "prop"},
	{Name: "套餐九折卡", MinUSD: 0, MaxUSD: 0, Probability: 0.012, RewardType: "prop"},
	{Name: "0.95 倍率卡", MinUSD: 0, MaxUSD: 0, Probability: 0.038, RewardType: "prop"},
	{Name: "0.9 倍率卡", MinUSD: 0, MaxUSD: 0, Probability: 0.022, RewardType: "prop"},
}

var currentSetting = Setting{
	Enabled:                      false,
	UnitPrice:                    2.5,
	ExpireDays:                   7,
	DailyLimit:                   50,
	MonthlyLimit:                 500,
	DailyOpenLimit:               5000,
	FirstPurchaseGuaranteeUSD:    20,
	PityThreshold:                5,
	PityGuaranteeUSD:             20,
	LowRewardThresholdUSD:        20,
	SubscriptionPrizeProbability: defaultSubscriptionPrizeProbability,
	SubscriptionPlanTitle:        defaultSubscriptionPlanTitle,
	CountOptions:                 []int{1, 5, 10, 20, 50},
	Tiers:                        append([]TierSetting(nil), defaultTierSettings...),
}

func init() {
	config.GlobalConfig.Register("blind_box_setting", &currentSetting)
}

func normalizeCountOptions(options []int) []int {
	if len(options) == 0 {
		return []int{1, 5, 10, 20, 50}
	}
	seen := make(map[int]struct{}, len(options))
	result := make([]int, 0, len(options))
	for _, option := range options {
		if option <= 0 {
			continue
		}
		if _, ok := seen[option]; ok {
			continue
		}
		seen[option] = struct{}{}
		result = append(result, option)
	}
	if len(result) == 0 {
		return []int{1, 5, 10, 20, 50}
	}
	sort.Ints(result)
	return result
}

func defaultTiers() []TierSetting {
	copied := make([]TierSetting, len(defaultTierSettings))
	copy(copied, defaultTierSettings)
	return copied
}

func isApproxProbability(left, right float64) bool {
	return math.Abs(left-right) < 0.0001
}

func isLegacyBrokenTiers(tiers []TierSetting) bool {
	legacyGroups := [][]TierSetting{
		{
			{Name: "5 美元普通额度", MinUSD: 5.0, MaxUSD: 5.0, Probability: 0.10},
			{Name: "8 美元普通额度", MinUSD: 8.0, MaxUSD: 8.0, Probability: 0.16},
			{Name: "12 美元普通额度", MinUSD: 12.0, MaxUSD: 12.0, Probability: 0.18},
			{Name: "20 美元 Claude 额度", MinUSD: 20.0, MaxUSD: 20.0, Probability: 0.20},
			{Name: "30 美元 Claude 额度", MinUSD: 30.0, MaxUSD: 30.0, Probability: 0.14},
			{Name: "充值九折卡", MinUSD: 0, MaxUSD: 0, Probability: 0.08},
			{Name: "套餐九折卡", MinUSD: 0, MaxUSD: 0, Probability: 0.07},
			{Name: "0.95 倍率卡", MinUSD: 0, MaxUSD: 0, Probability: 0.04},
			{Name: "0.9 倍率卡", MinUSD: 0, MaxUSD: 0, Probability: 0.03},
			{Name: "免费调用次数卡（10 次）", MinUSD: 0, MaxUSD: 0, Probability: 0.02},
		},
		{
			{Name: "5 美元普通额度", MinUSD: 5.0, MaxUSD: 5.0, Probability: 0.05},
			{Name: "8 美元普通额度", MinUSD: 8.0, MaxUSD: 8.0, Probability: 0.09},
			{Name: "12 美元普通额度", MinUSD: 12.0, MaxUSD: 12.0, Probability: 0.167},
			{Name: "20 美元 Claude 额度", MinUSD: 20.0, MaxUSD: 20.0, Probability: 0.23},
			{Name: "30 美元 Claude 额度", MinUSD: 30.0, MaxUSD: 30.0, Probability: 0.17},
			{Name: "充值九折卡", MinUSD: 0, MaxUSD: 0, Probability: 0.08},
			{Name: "套餐九折卡", MinUSD: 0, MaxUSD: 0, Probability: 0.07},
			{Name: "0.95 倍率卡", MinUSD: 0, MaxUSD: 0, Probability: 0.05},
			{Name: "0.9 倍率卡", MinUSD: 0, MaxUSD: 0, Probability: 0.04},
			{Name: "免费调用次数卡（10 次）", MinUSD: 0, MaxUSD: 0, Probability: 0.05},
		},
	}
	for _, legacy := range legacyGroups {
		if len(tiers) != len(legacy) {
			continue
		}
		matched := true
		for index, tier := range tiers {
			target := legacy[index]
			if strings.TrimSpace(tier.Name) != target.Name {
				matched = false
				break
			}
			if !isApproxProbability(tier.MinUSD, target.MinUSD) ||
				!isApproxProbability(tier.MaxUSD, target.MaxUSD) ||
				!isApproxProbability(tier.Probability, target.Probability) {
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

func normalizeWalletType(walletType string) string {
	switch strings.TrimSpace(walletType) {
	case "claude":
		return "claude"
	default:
		return "default"
	}
}

func inferRewardType(tier TierSetting) string {
	switch strings.TrimSpace(tier.RewardType) {
	case "claude_quota":
		return "claude_quota"
	case "prop":
		return "prop"
	case "subscription":
		return "subscription"
	case "quota":
		return "quota"
	}

	lowerName := strings.ToLower(strings.TrimSpace(tier.Name))
	if tier.MinUSD == 0 && tier.MaxUSD == 0 {
		return "prop"
	}
	if strings.Contains(lowerName, "claude") {
		return "claude_quota"
	}
	return "quota"
}

func inferWalletType(tier TierSetting) string {
	if normalizeWalletType(tier.WalletType) == "claude" {
		return "claude"
	}
	if inferRewardType(tier) == "claude_quota" {
		return "claude"
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(tier.Name)), "claude") {
		return "claude"
	}
	return "default"
}

func normalizeTierSettings(tiers []TierSetting) []TierSetting {
	if len(tiers) == 0 {
		return defaultTiers()
	}
	result := make([]TierSetting, len(tiers))
	for i, tier := range tiers {
		result[i] = tier
		result[i].RewardType = NormalizeRewardType(inferRewardType(tier))
		result[i].WalletType = inferWalletType(tier)
	}
	return result
}

// NormalizeRewardType maps persisted blind-box reward types onto supported values.
func NormalizeRewardType(rewardType string) string {
	switch strings.TrimSpace(rewardType) {
	case "claude_quota":
		return "claude_quota"
	case "prop":
		return "prop"
	case "subscription":
		return "subscription"
	default:
		return "quota"
	}
}

// Get returns the normalized blind-box setting snapshot.
func Get() Setting {
	settingCopy := currentSetting
	if settingCopy.UnitPrice <= 0 {
		settingCopy.UnitPrice = 2.5
	}
	if settingCopy.ExpireDays <= 0 {
		settingCopy.ExpireDays = 7
	}
	if settingCopy.DailyLimit <= 0 {
		settingCopy.DailyLimit = 50
	}
	if settingCopy.MonthlyLimit <= 0 {
		settingCopy.MonthlyLimit = 500
	}
	if settingCopy.DailyOpenLimit <= 0 {
		settingCopy.DailyOpenLimit = 5000
	}
	if settingCopy.FirstPurchaseGuaranteeUSD <= 0 {
		settingCopy.FirstPurchaseGuaranteeUSD = 10
	}
	if settingCopy.PityThreshold <= 0 {
		settingCopy.PityThreshold = 5
	}
	if settingCopy.PityGuaranteeUSD <= 0 {
		settingCopy.PityGuaranteeUSD = 10
	}
	if settingCopy.LowRewardThresholdUSD <= 0 {
		settingCopy.LowRewardThresholdUSD = 5
	}
	if settingCopy.SubscriptionPrizeProbability < 0 {
		settingCopy.SubscriptionPrizeProbability = 0
	}
	if settingCopy.SubscriptionPrizeProbability > 1 {
		settingCopy.SubscriptionPrizeProbability = 1
	}
	if settingCopy.SubscriptionPlanTitle == "" {
		settingCopy.SubscriptionPlanTitle = defaultSubscriptionPlanTitle
	}
	settingCopy.CountOptions = normalizeCountOptions(settingCopy.CountOptions)
	if len(settingCopy.Tiers) == 0 {
		settingCopy.Tiers = defaultTiers()
	}
	if isLegacyBrokenTiers(settingCopy.Tiers) {
		if strings.TrimSpace(settingCopy.SubscriptionPlanTitle) == "" ||
			strings.TrimSpace(settingCopy.SubscriptionPlanTitle) == "Standard月卡" {
			settingCopy.SubscriptionPlanTitle = defaultSubscriptionPlanTitle
		}
		if settingCopy.SubscriptionPrizeProbability <= 0 ||
			isApproxProbability(settingCopy.SubscriptionPrizeProbability, 0.001) {
			settingCopy.SubscriptionPrizeProbability = defaultSubscriptionPrizeProbability
		}
		if settingCopy.FirstPurchaseGuaranteeUSD <= 0 ||
			isApproxProbability(settingCopy.FirstPurchaseGuaranteeUSD, 10) {
			settingCopy.FirstPurchaseGuaranteeUSD = currentSetting.FirstPurchaseGuaranteeUSD
		}
		if settingCopy.PityGuaranteeUSD <= 0 ||
			isApproxProbability(settingCopy.PityGuaranteeUSD, 10) {
			settingCopy.PityGuaranteeUSD = currentSetting.PityGuaranteeUSD
		}
		if settingCopy.LowRewardThresholdUSD <= 0 ||
			isApproxProbability(settingCopy.LowRewardThresholdUSD, 5) {
			settingCopy.LowRewardThresholdUSD = currentSetting.LowRewardThresholdUSD
		}
		settingCopy.Tiers = defaultTiers()
	}
	settingCopy.Tiers = normalizeTierSettings(settingCopy.Tiers)
	return settingCopy
}

// Set replaces the in-memory blind-box setting snapshot.
func Set(setting Setting) {
	currentSetting = setting
}
