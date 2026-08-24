package blindboxsettings

var defaultTierSettings = []TierSetting{
	{Name: "0.20-0.80 统一额度", MinUSD: 0.2, MaxUSD: 0.8, Probability: 0.52177312, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "0.80-1.50 统一额度", MinUSD: 0.8, MaxUSD: 1.5, Probability: 0, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "1.50-2.50 统一额度", MinUSD: 1.5, MaxUSD: 2.5, Probability: 0.0107387, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "2.50-3.69 统一额度", MinUSD: 2.5, MaxUSD: 3.69, Probability: 0.30367707, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "4.50-12.00 统一额度", MinUSD: 4.5, MaxUSD: 12, Probability: 0.15, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "12.00-30.00 统一额度", MinUSD: 12, MaxUSD: 30, Probability: 0.0001, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "30.00-100.00 统一额度", MinUSD: 30, MaxUSD: 100, Probability: 0.00001, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "100.00-300.00 统一额度", MinUSD: 100, MaxUSD: 300, Probability: 0.000001, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "300.00-500.00 统一额度", MinUSD: 300, MaxUSD: 500, Probability: 0.0000001, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "500.00 统一额度", MinUSD: 500, MaxUSD: 500, Probability: 0.00000001, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "再来一抽", Probability: 0.0127, RewardType: "prop"},
	{Name: "15 分钟 0.1 倍率卡", Probability: 0.001, RewardType: "prop"},
}

var defaultBalanceBlindBoxTiers = copyTierSettings(defaultTierSettings)

var defaultBalanceBlindBoxFirstDrawTiers = []TierSetting{
	{Name: "首购 2.50-2.80 统一额度", MinUSD: 2.5, MaxUSD: 2.8, Probability: 0.70, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "首购 2.80-3.20 统一额度", MinUSD: 2.8, MaxUSD: 3.2, Probability: 0.25, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "首购 3.20-3.50 统一额度", MinUSD: 3.2, MaxUSD: 3.5, Probability: 0.05, RewardType: "claude_quota", WalletType: "claude"},
}

var defaultBalanceBlindBoxSmallPityTiers = []TierSetting{
	{Name: "小保底 2.50-3.00 统一额度", MinUSD: 2.5, MaxUSD: 3, Probability: 0.65, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "小保底 3.00-4.00 统一额度", MinUSD: 3, MaxUSD: 4, Probability: 0.25, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "小保底 4.00-6.00 统一额度", MinUSD: 4, MaxUSD: 6, Probability: 0.10, RewardType: "claude_quota", WalletType: "claude"},
}

var defaultBalanceBlindBoxPityTiers = []TierSetting{
	{Name: "大保底 8.75-10.00 统一额度", MinUSD: 8.75, MaxUSD: 10, Probability: 0.65, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "大保底 10.00-14.00 统一额度", MinUSD: 10, MaxUSD: 14, Probability: 0.25, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "大保底 14.00-20.00 统一额度", MinUSD: 14, MaxUSD: 20, Probability: 0.10, RewardType: "claude_quota", WalletType: "claude"},
}

var legacyBalanceBlindBoxProbabilities = [][]float64{
	{0.29311, 0.13, 0.08, 0.429317727273, 0.045, 0.008, 0.0007, 0.00015, 0.00002, 0.000002272727, 0.0127, 0.001},
	{0.30, 0.17, 0.10, 0.35, 0.055, 0.01, 0.001, 0.00025, 0.00004, 0.00001, 0.0127, 0.001},
	{0.2066, 0.24, 0.0975, 0.2759, 0.02, 0.012, 0.006, 0.0015, 0.0004, 0.0001, 0.06, 0.04, 0.01, 0.03},
	{0.4366, 0.24, 0.0975, 0.045, 0.02, 0.012, 0.006, 0.002, 0.0007, 0.0002, 0.06, 0.04, 0.01, 0.03},
	{0.1537, 0.27, 0.25, 0.12, 0.05, 0.01115, 0.004, 0.0008, 0.0003, 0.00005, 0.06, 0.04, 0.01, 0.03},
	{0.12, 0.17, 0.10, 0.075, 0.19, 0.025, 0.004, 0.00075, 0.00036, 0.00004, 0.075, 0.19, 0.025, 0.004, 0.00075, 0.007, 0.004, 0.006, 0.0031},
	{0.35, 0.18, 0.10, 0.10, 0.18, 0.04, 0.025, 0.006, 0.0015, 0.001, 0.002, 0.001, 0.0005, 0.0003, 0.0001, 0.0045, 0.0025, 0.0035, 0.0021},
	{0.12, 0.16, 0.18, 0.18, 0.20127, 0.03, 0.006, 0.001, 0.0002, 0.00003, 0.04, 0.035, 0.02, 0.008, 0.002, 0.0065, 0.0035, 0.0045, 0.002},
	{0.08, 0.12, 0.16, 0.20, 0.25, 0.03, 0.0043, 0.00058, 0.0001, 0.00002, 0.04, 0.035, 0.02, 0.008, 0.002, 0.0065, 0.0035, 0.0045, 0.002},
	{0.17, 0.145, 0.1719, 0.20, 0.20, 0.03, 0.0043, 0.00058, 0.0001, 0.00002, 0.015, 0.008, 0.004, 0.001, 0.0001, 0.0065, 0.0035, 0.0045, 0.002},
	{0.45197, 0.18, 0.09, 0.07, 0.05, 0.075, 0.025, 0.00625, 0.0015, 0.00088, 0.002, 0.001, 0.0005, 0.0003, 0.0001, 0.0065, 0.0035, 0.0045, 0.002},
	{0.45057, 0.18, 0.09, 0.07, 0.05, 0.075, 0.025, 0.00675, 0.002, 0.00128, 0.002, 0.001, 0.0005, 0.0003, 0.0001, 0.0065, 0.0035, 0.0045, 0.002},
}

func copyTierSettings(tiers []TierSetting) []TierSetting {
	copied := make([]TierSetting, len(tiers))
	copy(copied, tiers)
	return copied
}
