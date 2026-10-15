package schema

import (
	"time"

	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	GroupBuyStatusPending   = "pending"
	GroupBuyStatusCompleted = "completed"
	GroupBuyStatusExpired   = "expired"
)

type GroupBuyOrder struct {
	Id           int64  `json:"id"`
	InitiatorId  int    `json:"initiator_id" gorm:"type:int;not null;index"`
	PlanId       int    `json:"plan_id" gorm:"type:int;not null;index"`
	Status       string `json:"status" gorm:"type:varchar(20);not null;default:'pending';index"`
	TargetCount  int    `json:"target_count" gorm:"type:int;not null;default:5"`
	CurrentCount int    `json:"current_count" gorm:"type:int;not null;default:1"`
	ExpiresAt    int64  `json:"expires_at" gorm:"type:bigint;not null;index"`
	SettledAt    int64  `json:"settled_at" gorm:"type:bigint;default:0"`
	CreatedAt    int64  `json:"created_at" gorm:"type:bigint;not null"`
	UpdatedAt    int64  `json:"updated_at" gorm:"type:bigint;not null"`
}

func (g *GroupBuyOrder) BeforeCreate(tx *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	if g.Status == "" {
		g.Status = GroupBuyStatusPending
	}
	if g.TargetCount <= 0 {
		g.TargetCount = 5
	}
	if g.CurrentCount <= 0 {
		g.CurrentCount = 1
	}
	if g.ExpiresAt <= 0 {
		g.ExpiresAt = time.Now().Add(48 * time.Hour).Unix()
	}
	g.CreatedAt = now
	g.UpdatedAt = now
	return nil
}

func (g *GroupBuyOrder) BeforeUpdate(tx *gorm.DB) error {
	g.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}

type GroupBuyMember struct {
	Id                 int64   `json:"id"`
	GroupBuyId         int64   `json:"group_buy_id" gorm:"type:bigint;not null;uniqueIndex:idx_group_buy_user"`
	UserId             int     `json:"user_id" gorm:"type:int;not null;uniqueIndex:idx_group_buy_user;index"`
	OrderId            int     `json:"order_id" gorm:"type:int;not null;default:0;index"`
	UserSubscriptionId int     `json:"user_subscription_id" gorm:"type:int;not null;default:0;index"`
	BonusGranted       bool    `json:"bonus_granted" gorm:"default:false"`
	BonusAmountUSD     float64 `json:"bonus_amount_usd" gorm:"type:decimal(10,2);default:0"`
	CreatedAt          int64   `json:"created_at" gorm:"type:bigint;not null"`
}

func (m *GroupBuyMember) BeforeCreate(tx *gorm.DB) error {
	m.CreatedAt = platformruntime.GetTimestamp()
	return nil
}
