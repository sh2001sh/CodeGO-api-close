package schema

import (
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	BalanceBlindBoxSimulationStatusActive  = "active"
	BalanceBlindBoxSimulationStatusExpired = "expired"
	BalanceBlindBoxSimulationStatusStopped = "stopped"
)

// BalanceBlindBoxSimulationSession isolates temporary test draws from real balances.
type BalanceBlindBoxSimulationSession struct {
	Id          int    `json:"id"`
	UserId      int    `json:"user_id" gorm:"not null;index:idx_balance_box_sim_user_status,priority:1"`
	AdminUserId int    `json:"admin_user_id" gorm:"not null;index"`
	Status      string `json:"status" gorm:"type:varchar(24);not null;index:idx_balance_box_sim_user_status,priority:2"`
	Reason      string `json:"reason" gorm:"type:varchar(255);not null"`

	StartsAt  int64 `json:"starts_at" gorm:"type:bigint;not null"`
	ExpiresAt int64 `json:"expires_at" gorm:"type:bigint;not null;index"`

	DrawCount               int     `json:"draw_count" gorm:"not null;default:0"`
	SimulatedCostUSD        float64 `json:"simulated_cost_usd" gorm:"not null;default:0"`
	SimulatedRewardValueUSD float64 `json:"simulated_reward_value_usd" gorm:"not null;default:0"`
	ConsecutiveUnder6USD    int     `json:"-" gorm:"not null;default:0"`
	ConsecutiveUnder35USD   int     `json:"-" gorm:"not null;default:0"`
	FirstDrawEligible       bool    `json:"-" gorm:"not null;default:false"`
	CreatedAt               int64   `json:"created_at" gorm:"type:bigint;not null"`
	UpdatedAt               int64   `json:"updated_at" gorm:"type:bigint;not null"`
}

// BalanceBlindBoxSimulationBatch stores an idempotent simulated response.
type BalanceBlindBoxSimulationBatch struct {
	Id          int    `json:"id"`
	SessionId   int    `json:"session_id" gorm:"not null;index"`
	UserId      int    `json:"user_id" gorm:"not null;index"`
	RequestId   string `json:"request_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	Count       int    `json:"count" gorm:"not null"`
	ResultsJSON string `json:"-" gorm:"type:text;not null"`
	CreatedAt   int64  `json:"created_at" gorm:"type:bigint;not null;index"`
}

func (s *BalanceBlindBoxSimulationSession) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	s.CreatedAt, s.UpdatedAt = now, now
	if s.Status == "" {
		s.Status = BalanceBlindBoxSimulationStatusActive
	}
	return nil
}

func (s *BalanceBlindBoxSimulationSession) BeforeUpdate(_ *gorm.DB) error {
	s.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}

func (b *BalanceBlindBoxSimulationBatch) BeforeCreate(_ *gorm.DB) error {
	b.CreatedAt = platformruntime.GetTimestamp()
	return nil
}
