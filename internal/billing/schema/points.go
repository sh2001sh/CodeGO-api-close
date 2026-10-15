package billingschema

import (
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	PointLedgerTypeEarn    = "earn"
	PointLedgerTypeSpend   = "spend"
	PointLedgerTypeFreeze  = "freeze"
	PointLedgerTypeRelease = "release"
	PointLedgerTypeRefund  = "refund"

	PointSourcePackagePurchase            = "package_purchase"
	PointSourceBonusConversion            = "bonus_quota_conversion"
	PointSourceReferralRegister           = "referral_register"
	PointSourceReferralCall               = "referral_first_call"
	PointSourceReferralTopup              = "referral_first_topup"
	PointSourceReferralFirstPurchaseBonus = "referral_first_purchase_bonus"
	PointSourceReferralSpend7             = "referral_7_day_spend"
	PointSourceReferralRetain30           = "referral_30_day_retention"
	PointSourceAdminAdjust                = "admin_adjust"

	BonusQuotaStatusActive    = "active"
	BonusQuotaStatusExhausted = "exhausted"
)

type PointAccount struct {
	Id            int   `json:"id"`
	UserId        int   `json:"user_id" gorm:"uniqueIndex;not null"`
	Balance       int64 `json:"balance" gorm:"type:bigint;not null;default:0"`
	FrozenBalance int64 `json:"frozen_balance" gorm:"type:bigint;not null;default:0"`
	CreatedAt     int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt     int64 `json:"updated_at" gorm:"bigint"`
}

type PointLedger struct {
	Id             int    `json:"id"`
	UserId         int    `json:"user_id" gorm:"index;not null"`
	Type           string `json:"type" gorm:"type:varchar(32);index;not null"`
	Delta          int64  `json:"delta" gorm:"type:bigint;not null;default:0"`
	BalanceAfter   int64  `json:"balance_after" gorm:"type:bigint;not null;default:0"`
	FrozenAfter    int64  `json:"frozen_after" gorm:"type:bigint;not null;default:0"`
	SourceType     string `json:"source_type" gorm:"type:varchar(64);index;not null"`
	SourceId       string `json:"source_id" gorm:"type:varchar(128);index;default:''"`
	IdempotencyKey string `json:"idempotency_key" gorm:"type:varchar(160);uniqueIndex;not null"`
	Note           string `json:"note" gorm:"type:varchar(255);default:''"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint;index"`
}

type BonusQuotaCredit struct {
	Id              int    `json:"id"`
	UserId          int    `json:"user_id" gorm:"index;not null"`
	OriginalAmount  int64  `json:"original_amount" gorm:"type:bigint;not null;default:0"`
	RemainingAmount int64  `json:"remaining_amount" gorm:"type:bigint;not null;default:0;index"`
	SourceType      string `json:"source_type" gorm:"type:varchar(64);index;not null"`
	SourceId        string `json:"source_id" gorm:"type:varchar(128);index;default:''"`
	IdempotencyKey  string `json:"idempotency_key" gorm:"type:varchar(160);uniqueIndex;not null"`
	Status          string `json:"status" gorm:"type:varchar(16);index;not null;default:'active'"`
	CreatedAt       int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt       int64  `json:"updated_at" gorm:"bigint"`
}

func (a *PointAccount) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	a.CreatedAt = now
	a.UpdatedAt = now
	return nil
}

func (a *PointAccount) BeforeUpdate(_ *gorm.DB) error {
	a.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}

func (l *PointLedger) BeforeCreate(_ *gorm.DB) error {
	l.CreatedAt = platformruntime.GetTimestamp()
	return nil
}

func (c *BonusQuotaCredit) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	c.CreatedAt = now
	c.UpdatedAt = now
	return nil
}

func (c *BonusQuotaCredit) BeforeUpdate(_ *gorm.DB) error {
	c.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}
