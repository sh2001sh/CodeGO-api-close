package store

import "github.com/sh2001sh/new-api/setting/config"

// CheckinSetting controls the identity check-in reward flow.
type CheckinSetting struct {
	Enabled  bool `json:"enabled"`
	MinQuota int  `json:"min_quota"`
	MaxQuota int  `json:"max_quota"`
}

var checkinSetting = CheckinSetting{
	Enabled:  false,
	MinQuota: 1000,
	MaxQuota: 10000,
}

func init() {
	config.GlobalConfig.Register("checkin_setting", &checkinSetting)
}

func GetCheckinSetting() *CheckinSetting {
	return &checkinSetting
}

func IsCheckinEnabled() bool {
	return checkinSetting.Enabled
}

func GetCheckinQuotaRange() (min, max int) {
	return checkinSetting.MinQuota, checkinSetting.MaxQuota
}
