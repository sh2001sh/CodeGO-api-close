package settings

import settingconfig "github.com/sh2001sh/new-api/setting/config"

type CommunityResourceSetting struct {
	RewardUSD float64 `json:"reward_usd"`
}

var current = CommunityResourceSetting{}

func init() {
	settingconfig.GlobalConfig.Register("community_resource_setting", &current)
}

func Get() *CommunityResourceSetting { return &current }
