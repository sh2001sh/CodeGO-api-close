package schema

import (
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

// ReferralPurchaseReward records an inviter reward for an invitee's first qualifying purchase.
type ReferralPurchaseReward struct {
	Id               int    `json:"id"`
	InviterId        int    `json:"inviter_id" gorm:"index;not null"`
	InviteeId        int    `json:"invitee_id" gorm:"uniqueIndex;not null"`
	PurchaseType     string `json:"purchase_type" gorm:"type:varchar(32);index;not null"`
	PurchaseLabel    string `json:"purchase_label" gorm:"type:varchar(64);not null;default:''"`
	BonusQuotaAmount int64  `json:"bonus_quota_amount" gorm:"type:bigint;not null;default:0"`
	OrderSourceType  string `json:"order_source_type" gorm:"type:varchar(32);index;not null;default:''"`
	OrderSourceId    string `json:"order_source_id" gorm:"type:varchar(128);index;not null;default:''"`
	RewardedAt       int64  `json:"rewarded_at" gorm:"bigint;index"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt        int64  `json:"updated_at" gorm:"bigint"`
}

func (reward *ReferralPurchaseReward) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	reward.CreatedAt = now
	reward.UpdatedAt = now
	if reward.RewardedAt == 0 {
		reward.RewardedAt = now
	}
	return nil
}

func (reward *ReferralPurchaseReward) BeforeUpdate(_ *gorm.DB) error {
	reward.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}
