package luckysettings

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sh2001sh/new-api/setting/config"
)

const DefaultTimezone = "Asia/Shanghai"

// Setting is the configuration snapshot used when the next draw is created.
type Setting struct {
	Enabled             bool    `json:"enabled"`
	Timezone            string  `json:"timezone"`
	DrawHour            int     `json:"draw_hour"`
	DrawMinute          int     `json:"draw_minute"`
	BaseReward1USD      float64 `json:"base_reward_1_usd"`
	BaseReward2USD      float64 `json:"base_reward_2_usd"`
	BaseReward3USD      float64 `json:"base_reward_3_usd"`
	BaseReward4USD      float64 `json:"base_reward_4_usd"`
	MultiplierLite      float64 `json:"multiplier_lite"`
	MultiplierStandard  float64 `json:"multiplier_standard"`
	MultiplierPro       float64 `json:"multiplier_pro"`
	MultiplierUltra     float64 `json:"multiplier_ultra"`
	JackpotInitialUSD   float64 `json:"jackpot_initial_usd"`
	JackpotIncrementUSD float64 `json:"jackpot_increment_usd"`
	JackpotCapUSD       float64 `json:"jackpot_cap_usd"`
	CostPerUSD          float64 `json:"cost_per_usd"`
	MonthlyBudgetUSD    float64 `json:"monthly_budget_usd"`
}

var (
	settingMu sync.RWMutex
	current   = Setting{
		Enabled:             true,
		Timezone:            DefaultTimezone,
		DrawHour:            20,
		DrawMinute:          0,
		BaseReward1USD:      1,
		BaseReward2USD:      10,
		BaseReward3USD:      50,
		BaseReward4USD:      100,
		MultiplierLite:      1,
		MultiplierStandard:  1.1,
		MultiplierPro:       1.2,
		MultiplierUltra:     1.3,
		JackpotInitialUSD:   100,
		JackpotIncrementUSD: 20,
		JackpotCapUSD:       1000,
		CostPerUSD:          0.1,
		MonthlyBudgetUSD:    0,
	}
)

func init() {
	config.GlobalConfig.Register("daily_lucky_number_setting", &current)
}

// Get returns a normalized runtime copy of the current setting.
func Get() Setting {
	settingMu.RLock()
	value := current
	settingMu.RUnlock()
	return Normalize(value)
}

// Set replaces the runtime setting after validation and normalization.
func Set(value Setting) error {
	if err := Validate(value); err != nil {
		return err
	}
	settingMu.Lock()
	current = Normalize(value)
	settingMu.Unlock()
	return nil
}

// ApplyField applies one persisted option to a validated copy of the setting.
// It keeps option synchronization from mutating the live setting piecemeal.
func ApplyField(key, value string) error {
	settingMu.RLock()
	candidate := current
	settingMu.RUnlock()
	if err := config.UpdateConfigFromMap(&candidate, map[string]string{key: value}); err != nil {
		return err
	}
	return Set(candidate)
}

// Normalize fills defaults while keeping valid explicit zero-hour schedules.
func Normalize(value Setting) Setting {
	if strings.TrimSpace(value.Timezone) == "" {
		value.Timezone = DefaultTimezone
	}
	if _, err := time.LoadLocation(value.Timezone); err != nil {
		value.Timezone = DefaultTimezone
	}
	if value.DrawHour < 0 || value.DrawHour > 23 {
		value.DrawHour = 20
	}
	if value.DrawMinute < 0 || value.DrawMinute > 59 {
		value.DrawMinute = 0
	}
	if value.BaseReward1USD <= 0 {
		value.BaseReward1USD = 1
	}
	if value.BaseReward2USD <= 0 {
		value.BaseReward2USD = 10
	}
	if value.BaseReward3USD <= 0 {
		value.BaseReward3USD = 50
	}
	if value.BaseReward4USD <= 0 {
		value.BaseReward4USD = 100
	}
	if value.MultiplierLite <= 0 {
		value.MultiplierLite = 1
	}
	if value.MultiplierStandard <= 0 {
		value.MultiplierStandard = 1.1
	}
	if value.MultiplierPro <= 0 {
		value.MultiplierPro = 1.2
	}
	if value.MultiplierUltra <= 0 {
		value.MultiplierUltra = 1.3
	}
	if value.JackpotInitialUSD <= 0 {
		value.JackpotInitialUSD = 100
	}
	if value.JackpotIncrementUSD < 0 {
		value.JackpotIncrementUSD = 20
	}
	if value.JackpotCapUSD < value.JackpotInitialUSD {
		value.JackpotCapUSD = value.JackpotInitialUSD
	}
	if value.CostPerUSD < 0 {
		value.CostPerUSD = 0.1
	}
	if value.MonthlyBudgetUSD < 0 {
		value.MonthlyBudgetUSD = 0
	}
	return value
}

func Validate(value Setting) error {
	if strings.TrimSpace(value.Timezone) == "" {
		return fmt.Errorf("timezone is required")
	}
	if _, err := time.LoadLocation(value.Timezone); err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}
	if value.DrawHour < 0 || value.DrawHour > 23 || value.DrawMinute < 0 || value.DrawMinute > 59 {
		return fmt.Errorf("draw time is invalid")
	}
	if value.BaseReward1USD <= 0 || value.BaseReward2USD <= 0 || value.BaseReward3USD <= 0 || value.BaseReward4USD <= 0 {
		return fmt.Errorf("base rewards must be positive")
	}
	if value.MultiplierLite <= 0 || value.MultiplierStandard <= 0 || value.MultiplierPro <= 0 || value.MultiplierUltra <= 0 {
		return fmt.Errorf("tier multipliers must be positive")
	}
	if value.JackpotInitialUSD <= 0 || value.JackpotIncrementUSD < 0 || value.JackpotCapUSD < value.JackpotInitialUSD {
		return fmt.Errorf("jackpot configuration is invalid")
	}
	if value.CostPerUSD < 0 || value.MonthlyBudgetUSD < 0 {
		return fmt.Errorf("cost and budget must be non-negative")
	}
	return nil
}

func (value Setting) Location() (*time.Location, error) {
	return time.LoadLocation(Normalize(value).Timezone)
}
