package blindboxsettings

var defaultTierSettings = []TierSetting{
	{Name: "1.00-1.50 统一额度", MinUSD: 1, MaxUSD: 1.5, Probability: 0.2066, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "1.50-2.00 统一额度", MinUSD: 1.5, MaxUSD: 2, Probability: 0.24, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "2.00-2.80 统一额度", MinUSD: 2, MaxUSD: 2.8, Probability: 0.0975, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "2.80-4.00 统一额度", MinUSD: 2.8, MaxUSD: 4, Probability: 0.2759, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "4.00-6.00 统一额度", MinUSD: 4, MaxUSD: 6, Probability: 0.02, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "6.00-10.00 统一额度", MinUSD: 6, MaxUSD: 10, Probability: 0.012, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "10.00-20.00 统一额度", MinUSD: 10, MaxUSD: 20, Probability: 0.006, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "20.00-50.00 统一额度", MinUSD: 20, MaxUSD: 50, Probability: 0.0015, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "50.00-200.00 统一额度", MinUSD: 50, MaxUSD: 200, Probability: 0.0004, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "200.00-1000.00 统一额度", MinUSD: 200, MaxUSD: 1000, Probability: 0.0001, RewardType: "claude_quota", WalletType: "claude"},
	{Name: "0.95 倍率卡", Probability: 0.06, RewardType: "prop"},
	{Name: "0.9 倍率卡", Probability: 0.04, RewardType: "prop"},
	{Name: "0.1 倍率卡", Probability: 0.01, RewardType: "prop"},
	{Name: "充值九折卡", Probability: 0.03, RewardType: "prop"},
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
