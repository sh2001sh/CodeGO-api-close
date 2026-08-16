package blindboxsettings

import (
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
	BalanceBlindBoxFirstDrawTiers        []TierSetting `json:"balance_blind_box_first_draw_tiers"`
	BalanceBlindBoxSmallPityTiers        []TierSetting `json:"balance_blind_box_small_pity_tiers"`
	BalanceBlindBoxPityTiers             []TierSetting `json:"balance_blind_box_pity_tiers"`
}

const (
	defaultSubscriptionPrizeProbability = 0.003
	defaultSubscriptionPlanTitle        = "Lite月卡"
)

var currentSetting = Setting{
	Enabled:                              false,
	UnitPrice:                            2.5,
	ExpireDays:                           7,
	RegistrationRewardEnabled:            true,
	RegistrationRewardStartAt:            0,
	RegistrationRewardEndAt:              0,
	DailyLimit:                           10,
	MonthlyLimit:                         500,
	DailyOpenLimit:                       5000,
	FirstPurchaseGuaranteeUSD:            0,
	PityThreshold:                        1_000_000,
	PityGuaranteeUSD:                     0,
	LowRewardThresholdUSD:                1,
	SubscriptionPrizeProbability:         defaultSubscriptionPrizeProbability,
	SubscriptionPlanTitle:                defaultSubscriptionPlanTitle,
	CountOptions:                         []int{1, 5, 10},
	Tiers:                                append([]TierSetting(nil), defaultTierSettings...),
	BalanceBlindBoxEnabled:               true,
	BalanceBlindBoxPriceUSD:              2.5,
	BalanceBlindBoxDailyPurchaseLimit:    10,
	BalanceBlindBoxTiers:                 append([]TierSetting(nil), defaultBalanceBlindBoxTiers...),
	BalanceBlindBoxPityThreshold:         50,
	BalanceBlindBoxPityGuaranteeUSD:      35,
	BalanceBlindBoxSmallPityThreshold:    10,
	BalanceBlindBoxSmallPityGuaranteeUSD: 10,
	BalanceBlindBoxFirstDrawGuaranteeUSD: 10,
	BalanceBlindBoxFirstDrawTiers:        copyTierSettings(defaultBalanceBlindBoxFirstDrawTiers),
	BalanceBlindBoxSmallPityTiers:        copyTierSettings(defaultBalanceBlindBoxSmallPityTiers),
	BalanceBlindBoxPityTiers:             copyTierSettings(defaultBalanceBlindBoxPityTiers),
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
		return []int{1, 5, 10}
	}
	seen := make(map[int]struct{}, len(options))
	result := make([]int, 0, len(options))
	for _, option := range options {
		if option <= 0 || option > 10 {
			continue
		}
		if _, ok := seen[option]; ok {
			continue
		}
		seen[option] = struct{}{}
		result = append(result, option)
	}
	if len(result) == 0 {
		return []int{1, 5, 10}
	}
	sort.Ints(result)
	return result
}

func defaultTiers() []TierSetting {
	return copyTierSettings(defaultTierSettings)
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
		if rewardType == "prop" && (strings.TrimSpace(result[i].Name) == "0.10 倍率体验卡" || strings.TrimSpace(result[i].Name) == "0.1 倍率卡") {
			result[i].Name = "15 分钟 0.1 倍率卡"
		}
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
	if settingCopy.DailyLimit <= 0 || settingCopy.DailyLimit > 10 {
		settingCopy.DailyLimit = 10
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
	if settingCopy.BalanceBlindBoxPriceUSD <= 0 {
		settingCopy.BalanceBlindBoxPriceUSD = 2.5
	}
	if settingCopy.BalanceBlindBoxDailyPurchaseLimit <= 0 || settingCopy.BalanceBlindBoxDailyPurchaseLimit > 10 {
		settingCopy.BalanceBlindBoxDailyPurchaseLimit = 10
	}
	if settingCopy.BalanceBlindBoxPityThreshold <= 0 || settingCopy.BalanceBlindBoxPityThreshold >= 1_000_000 {
		settingCopy.BalanceBlindBoxPityThreshold = 50
	}
	if settingCopy.BalanceBlindBoxPityGuaranteeUSD <= 0 {
		settingCopy.BalanceBlindBoxPityGuaranteeUSD = 35
	}
	if settingCopy.BalanceBlindBoxSmallPityThreshold <= 0 || settingCopy.BalanceBlindBoxSmallPityThreshold >= 1_000_000 {
		settingCopy.BalanceBlindBoxSmallPityThreshold = 10
	}
	if settingCopy.BalanceBlindBoxSmallPityGuaranteeUSD <= 0 {
		settingCopy.BalanceBlindBoxSmallPityGuaranteeUSD = 10
	}
	if settingCopy.BalanceBlindBoxFirstDrawGuaranteeUSD <= 0 {
		settingCopy.BalanceBlindBoxFirstDrawGuaranteeUSD = 10
	}
	settingCopy.BalanceBlindBoxTiers = normalizeBalanceBlindBoxTiers(settingCopy)
	settingCopy.Tiers = copyTierSettings(settingCopy.BalanceBlindBoxTiers)
	settingCopy.BalanceBlindBoxFirstDrawTiers = normalizeGuaranteeTiers(
		settingCopy.BalanceBlindBoxFirstDrawTiers,
		defaultBalanceBlindBoxFirstDrawTiers,
	)
	settingCopy.BalanceBlindBoxSmallPityTiers = normalizeGuaranteeTiers(
		settingCopy.BalanceBlindBoxSmallPityTiers,
		defaultBalanceBlindBoxSmallPityTiers,
	)
	settingCopy.BalanceBlindBoxPityTiers = normalizeGuaranteeTiers(
		settingCopy.BalanceBlindBoxPityTiers,
		defaultBalanceBlindBoxPityTiers,
	)
	return settingCopy
}

func normalizeBalanceBlindBoxTiers(setting Setting) []TierSetting {
	balanceTiers := setting.BalanceBlindBoxTiers
	if len(balanceTiers) == 0 {
		balanceTiers = setting.Tiers
	}
	if isLegacyBalanceBlindBoxTiers(balanceTiers) {
		if !isLegacyBalanceBlindBoxTiers(setting.Tiers) {
			return normalizeTierSettings(setting.Tiers)
		}
		return copyTierSettings(defaultBalanceBlindBoxTiers)
	}
	return normalizeTierSettings(balanceTiers)
}

func normalizeGuaranteeTiers(tiers, defaults []TierSetting) []TierSetting {
	if len(tiers) == 0 {
		tiers = defaults
	}
	return normalizeTierSettings(copyTierSettings(tiers))
}

// Set replaces the in-memory blind-box setting snapshot.
func Set(setting Setting) {
	currentSetting = setting
}
