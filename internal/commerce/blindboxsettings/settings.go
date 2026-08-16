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
	Enabled                              bool          `json:"enabled"`
	UnitPrice                            float64       `json:"unit_price"`
	ExpireDays                           int           `json:"expire_days"`
	RegistrationRewardEnabled            bool          `json:"registration_reward_enabled"`
	RegistrationRewardStartAt            int64         `json:"registration_reward_start_at"`
	RegistrationRewardEndAt              int64         `json:"registration_reward_end_at"`
	DailyLimit                           int           `json:"daily_limit"`
	MonthlyLimit                         int           `json:"monthly_limit"`
	DailyOpenLimit                       int           `json:"daily_open_limit"`
	FirstPurchaseGuaranteeUSD            float64       `json:"first_purchase_guarantee_usd"`
	PityThreshold                        int           `json:"pity_threshold"`
	PityGuaranteeUSD                     float64       `json:"pity_guarantee_usd"`
	LowRewardThresholdUSD                float64       `json:"low_reward_threshold_usd"`
	SubscriptionPrizeProbability         float64       `json:"subscription_prize_probability"`
	SubscriptionPlanTitle                string        `json:"subscription_plan_title"`
	CountOptions                         []int         `json:"count_options"`
	Tiers                                []TierSetting `json:"tiers"`
	BalanceBlindBoxEnabled               bool          `json:"balance_blind_box_enabled"`
	BalanceBlindBoxPriceUSD              float64       `json:"balance_blind_box_price_usd"`
	BalanceBlindBoxDailyPurchaseLimit    int           `json:"balance_blind_box_daily_purchase_limit"`
	BalanceBlindBoxTiers                 []TierSetting `json:"balance_blind_box_tiers"`
	BalanceBlindBoxPityThreshold         int           `json:"balance_blind_box_pity_threshold"`
	BalanceBlindBoxPityGuaranteeUSD      float64       `json:"balance_blind_box_pity_guarantee_usd"`
	BalanceBlindBoxSmallPityThreshold    int           `json:"balance_blind_box_small_pity_threshold"`
	BalanceBlindBoxSmallPityGuaranteeUSD float64       `json:"balance_blind_box_small_pity_guarantee_usd"`
	BalanceBlindBoxFirstDrawGuaranteeUSD float64       `json:"balance_blind_box_first_draw_guarantee_usd"`
}

const (
	defaultSubscriptionPrizeProbability = 0.003
	defaultSubscriptionPlanTitle        = "Lite月卡"
)

var defaultTierSettings = []TierSetting{
	{Name: "1.00-1.50 统一额度", MinUSD: 1, MaxUSD: 1.5, Probability: 0.1537, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "1.50-2.00 统一额度", MinUSD: 1.5, MaxUSD: 2, Probability: 0.27, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "2.00-2.80 统一额度", MinUSD: 2, MaxUSD: 2.8, Probability: 0.25, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "2.80-4.00 统一额度", MinUSD: 2.8, MaxUSD: 4, Probability: 0.12, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "4.00-6.00 统一额度", MinUSD: 4, MaxUSD: 6, Probability: 0.05, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "6.00-10.00 统一额度", MinUSD: 6, MaxUSD: 10, Probability: 0.01115, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "10.00-20.00 统一额度", MinUSD: 10, MaxUSD: 20, Probability: 0.004, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "20.00-50.00 统一额度", MinUSD: 20, MaxUSD: 50, Probability: 0.0008, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "50.00-200.00 统一额度", MinUSD: 50, MaxUSD: 200, Probability: 0.0003, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "200.00-1000.00 统一额度", MinUSD: 200, MaxUSD: 1000, Probability: 0.00005, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "0.95 倍率卡", Probability: 0.06, RewardType: "prop"},
	{Name: "0.9 倍率卡", Probability: 0.04, RewardType: "prop"},
	{Name: "0.10 倍率体验卡", Probability: 0.01, RewardType: "prop"},
	{Name: "充值九折卡", Probability: 0.03, RewardType: "prop"},
}

var defaultBalanceBlindBoxTiers = append([]TierSetting(nil), defaultTierSettings...)

var legacyBalanceBlindBoxProbabilities = [][]float64{
	{0.12, 0.17, 0.10, 0.075, 0.19, 0.025, 0.004, 0.00075, 0.00036, 0.00004, 0.075, 0.19, 0.025, 0.004, 0.00075, 0.007, 0.004, 0.006, 0.0031},
	{0.35, 0.18, 0.10, 0.10, 0.18, 0.04, 0.025, 0.006, 0.0015, 0.001, 0.002, 0.001, 0.0005, 0.0003, 0.0001, 0.0045, 0.0025, 0.0035, 0.0021},
	{0.12, 0.16, 0.18, 0.18, 0.20127, 0.03, 0.006, 0.001, 0.0002, 0.00003, 0.04, 0.035, 0.02, 0.008, 0.002, 0.0065, 0.0035, 0.0045, 0.002},
	{0.08, 0.12, 0.16, 0.20, 0.25, 0.03, 0.0043, 0.00058, 0.0001, 0.00002, 0.04, 0.035, 0.02, 0.008, 0.002, 0.0065, 0.0035, 0.0045, 0.002},
	{0.17, 0.145, 0.1719, 0.20, 0.20, 0.03, 0.0043, 0.00058, 0.0001, 0.00002, 0.015, 0.008, 0.004, 0.001, 0.0001, 0.0065, 0.0035, 0.0045, 0.002},
	{0.45197, 0.18, 0.09, 0.07, 0.05, 0.075, 0.025, 0.00625, 0.0015, 0.00088, 0.002, 0.001, 0.0005, 0.0003, 0.0001, 0.0065, 0.0035, 0.0045, 0.002},
	{0.45057, 0.18, 0.09, 0.07, 0.05, 0.075, 0.025, 0.00675, 0.002, 0.00128, 0.002, 0.001, 0.0005, 0.0003, 0.0001, 0.0065, 0.0035, 0.0045, 0.002},
}

var currentSetting = Setting{
	Enabled:                              false,
	UnitPrice:                            2.5,
	ExpireDays:                           7,
	RegistrationRewardEnabled:            true,
	RegistrationRewardStartAt:            0,
	RegistrationRewardEndAt:              0,
	DailyLimit:                           50,
	MonthlyLimit:                         500,
	DailyOpenLimit:                       5000,
	FirstPurchaseGuaranteeUSD:            0,
	PityThreshold:                        1_000_000,
	PityGuaranteeUSD:                     0,
	LowRewardThresholdUSD:                1,
	SubscriptionPrizeProbability:         defaultSubscriptionPrizeProbability,
	SubscriptionPlanTitle:                defaultSubscriptionPlanTitle,
	CountOptions:                         []int{1, 5, 10, 20, 50},
	Tiers:                                append([]TierSetting(nil), defaultTierSettings...),
	BalanceBlindBoxEnabled:               true,
	BalanceBlindBoxPriceUSD:              2.5,
	BalanceBlindBoxDailyPurchaseLimit:    500,
	BalanceBlindBoxTiers:                 append([]TierSetting(nil), defaultBalanceBlindBoxTiers...),
	BalanceBlindBoxPityThreshold:         1_000_000,
	BalanceBlindBoxPityGuaranteeUSD:      0,
	BalanceBlindBoxSmallPityThreshold:    1_000_000,
	BalanceBlindBoxSmallPityGuaranteeUSD: 0,
	BalanceBlindBoxFirstDrawGuaranteeUSD: 0,
}

// RegistrationRewardActive reports whether invited registrations currently
// qualify for the configured blind-box campaign.
func (s *Setting) RegistrationRewardActive(now int64) bool {
	if s == nil || !s.RegistrationRewardEnabled {
		return false
	}
	if s.RegistrationRewardStartAt > 0 && now < s.RegistrationRewardStartAt {
		return false
	}
	return s.RegistrationRewardEndAt <= 0 || now < s.RegistrationRewardEndAt
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

func isLegacyBalanceBlindBoxTiers(tiers []TierSetting) bool {
	if len(tiers) != len(defaultBalanceBlindBoxTiers) {
		return false
	}
	for _, probabilities := range legacyBalanceBlindBoxProbabilities {
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
		rewardType := NormalizeRewardType(inferRewardType(tier))
		if rewardType == "quota" || rewardType == "claude_quota" {
			result[i].RewardType = "claude_quota"
			result[i].WalletType = "claude"
			continue
		}
		result[i].RewardType = rewardType
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
	settingCopy.FirstPurchaseGuaranteeUSD = 0
	settingCopy.PityThreshold = 1_000_000
	settingCopy.PityGuaranteeUSD = 0
	settingCopy.LowRewardThresholdUSD = 0
	settingCopy.BalanceBlindBoxPriceUSD = 2.5
	if settingCopy.BalanceBlindBoxDailyPurchaseLimit <= 0 {
		settingCopy.BalanceBlindBoxDailyPurchaseLimit = 10
	}
	if settingCopy.BalanceBlindBoxPityThreshold <= 0 {
		settingCopy.BalanceBlindBoxPityThreshold = 50
	}
	if settingCopy.BalanceBlindBoxPityGuaranteeUSD <= 0 {
		settingCopy.BalanceBlindBoxPityGuaranteeUSD = 35
	}
	if settingCopy.BalanceBlindBoxSmallPityThreshold <= 0 {
		settingCopy.BalanceBlindBoxSmallPityThreshold = 10
	}
	if settingCopy.BalanceBlindBoxSmallPityGuaranteeUSD <= 0 {
		settingCopy.BalanceBlindBoxSmallPityGuaranteeUSD = 10
	}
	if settingCopy.BalanceBlindBoxFirstDrawGuaranteeUSD <= 0 {
		settingCopy.BalanceBlindBoxFirstDrawGuaranteeUSD = 10
	}
	settingCopy.BalanceBlindBoxTiers = append([]TierSetting(nil), settingCopy.Tiers...)
	settingCopy.BalanceBlindBoxPityThreshold = 1_000_000
	settingCopy.BalanceBlindBoxPityGuaranteeUSD = 0
	settingCopy.BalanceBlindBoxSmallPityThreshold = 1_000_000
	settingCopy.BalanceBlindBoxSmallPityGuaranteeUSD = 0
	settingCopy.BalanceBlindBoxFirstDrawGuaranteeUSD = 0
	return settingCopy
}

// Set replaces the in-memory blind-box setting snapshot.
func Set(setting Setting) {
	currentSetting = setting
}
