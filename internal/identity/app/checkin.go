package app

import (
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	"errors"
	"fmt"

	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	"github.com/sh2001sh/new-api/internal/platform/logger"
)

var ErrCheckinDisabled = errors.New("签到功能未启用")

type CheckinStatusResponse struct {
	Enabled  bool           `json:"enabled"`
	MinQuota int            `json:"min_quota"`
	MaxQuota int            `json:"max_quota"`
	Stats    map[string]any `json:"stats"`
}

type CheckinResult struct {
	QuotaAwarded int    `json:"quota_awarded"`
	CheckinDate  string `json:"checkin_date"`
}

// LoadCheckinStatus returns the user's check-in configuration and monthly statistics.
func LoadCheckinStatus(userID int, month string) (*CheckinStatusResponse, error) {
	setting := identitystore.GetCheckinSetting()
	if !setting.Enabled {
		return nil, ErrCheckinDisabled
	}

	stats, err := loadUserCheckinStats(userID, month)
	if err != nil {
		return nil, err
	}

	return &CheckinStatusResponse{
		Enabled:  setting.Enabled,
		MinQuota: setting.MinQuota,
		MaxQuota: setting.MaxQuota,
		Stats:    stats,
	}, nil
}

// PerformCheckin executes the user's daily check-in and records the operation log.
func PerformCheckin(userID int) (*CheckinResult, error) {
	setting := identitystore.GetCheckinSetting()
	if !setting.Enabled {
		return nil, ErrCheckinDisabled
	}

	checkin, err := performUserCheckin(userID)
	if err != nil {
		return nil, err
	}

	auditapp.RecordLog(userID, auditschema.LogTypeSystem, fmt.Sprintf("用户签到，获得额度 %s", logger.LogQuota(checkin.QuotaAwarded)))
	return &CheckinResult{
		QuotaAwarded: checkin.QuotaAwarded,
		CheckinDate:  checkin.CheckinDate,
	}, nil
}
