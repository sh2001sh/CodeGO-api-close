package schema

import (
	"errors"

	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	SubscriptionResetOpportunityChangeEarn = "earn"
	SubscriptionResetOpportunityChangeUse  = "use"
)

var (
	ErrSubscriptionResetOpportunityUnavailable = errors.New("当前没有可用的额度重置机会")
	ErrSubscriptionResetOpportunityMonthlyUsed = errors.New("本月已经使用过一次额度重置机会")
	ErrSubscriptionResetOpportunityNoActiveSub = errors.New("当前没有可重置的生效订阅")
)

type SubscriptionResetOpportunityAccount struct {
	Id             int    `json:"id"`
	UserId         int    `json:"user_id" gorm:"uniqueIndex;not null"`
	EarnedTotal    int    `json:"earned_total" gorm:"not null;default:0"`
	UsedTotal      int    `json:"used_total" gorm:"not null;default:0"`
	AvailableTotal int    `json:"available_total" gorm:"not null;default:0"`
	LastUsedMonth  string `json:"last_used_month" gorm:"type:varchar(7);default:''"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt      int64  `json:"updated_at" gorm:"bigint"`
}

func (a *SubscriptionResetOpportunityAccount) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	a.CreatedAt = now
	a.UpdatedAt = now
	return nil
}

func (a *SubscriptionResetOpportunityAccount) BeforeUpdate(_ *gorm.DB) error {
	a.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}

type SubscriptionResetOpportunityLedger struct {
	Id            int    `json:"id"`
	UserId        int    `json:"user_id" gorm:"index;not null"`
	RelatedUserId int    `json:"related_user_id" gorm:"index;not null;default:0"`
	ChangeType    string `json:"change_type" gorm:"type:varchar(16);index;not null"`
	Delta         int    `json:"delta" gorm:"not null"`
	BalanceAfter  int    `json:"balance_after" gorm:"not null;default:0"`
	UsedMonth     string `json:"used_month" gorm:"type:varchar(7);index;default:''"`
	SourceType    string `json:"source_type" gorm:"type:varchar(32);index;not null;default:''"`
	SourceRef     string `json:"source_ref" gorm:"type:varchar(128);index;not null;default:''"`
	EventKey      string `json:"event_key" gorm:"type:varchar(128);uniqueIndex;not null"`
	Note          string `json:"note" gorm:"type:varchar(255);default:''"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint"`
}

func (l *SubscriptionResetOpportunityLedger) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	l.CreatedAt = now
	l.UpdatedAt = now
	return nil
}

func (l *SubscriptionResetOpportunityLedger) BeforeUpdate(_ *gorm.DB) error {
	l.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}

type SubscriptionResetOpportunitySummary struct {
	AvailableCount int    `json:"available_count"`
	EarnedTotal    int    `json:"earned_total"`
	UsedTotal      int    `json:"used_total"`
	UsedThisMonth  bool   `json:"used_this_month"`
	CurrentMonth   string `json:"current_month"`
	LastUsedMonth  string `json:"last_used_month"`
}

type SubscriptionResetOpportunityUseResult struct {
	ResetOpportunity   SubscriptionResetOpportunitySummary `json:"reset_opportunity"`
	UserSubscriptionId int                                 `json:"subscription_id"`
	AmountUsedBefore   int64                               `json:"amount_used_before"`
	AmountUsedAfter    int64                               `json:"amount_used_after"`
	PeriodUsedBefore   int64                               `json:"period_used_before"`
	PeriodUsedAfter    int64                               `json:"period_used_after"`
	ClearedUsedAmount  int64                               `json:"cleared_used_amount"`
}
