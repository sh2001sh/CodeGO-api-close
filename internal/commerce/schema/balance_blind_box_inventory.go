package schema

import (
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	BalanceBlindBoxItemStatusAvailable = "available"
	BalanceBlindBoxItemStatusOpened    = "opened"
	BalanceBlindBoxPurchaseCompleted   = "completed"
	BalanceBlindBoxGiftCompleted       = "completed"
)

// BalanceBlindBoxPurchase records one paid issuance batch.
type BalanceBlindBoxPurchase struct {
	Id           int     `json:"id"`
	UserId       int     `json:"-" gorm:"not null;index:idx_balance_box_purchase_user_date,priority:1"`
	RequestId    string  `json:"request_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	Quantity     int     `json:"quantity" gorm:"not null"`
	UnitPriceUSD float64 `json:"unit_price_usd" gorm:"not null"`
	TotalQuota   int64   `json:"total_quota" gorm:"type:bigint;not null"`
	PurchaseDate string  `json:"purchase_date" gorm:"type:varchar(10);not null;index:idx_balance_box_purchase_user_date,priority:2"`
	Status       string  `json:"status" gorm:"type:varchar(24);not null"`
	CreatedAt    int64   `json:"created_at" gorm:"type:bigint;not null;index"`
}

// BalanceBlindBoxItem is a sealed, transferable balance blind box.
type BalanceBlindBoxItem struct {
	Id               int     `json:"id"`
	PurchaseId       int     `json:"purchase_id" gorm:"not null;index"`
	PurchaseUserId   int     `json:"-" gorm:"not null;index"`
	OwnerUserId      int     `json:"-" gorm:"not null;index:idx_balance_box_owner_status,priority:1"`
	PoolVersion      string  `json:"pool_version" gorm:"type:varchar(48);not null"`
	RewardType       string  `json:"-" gorm:"type:varchar(32);not null"`
	RewardTier       string  `json:"-" gorm:"type:varchar(96);not null"`
	RewardUSD        float64 `json:"-" gorm:"not null"`
	CreditAmount     int64   `json:"-" gorm:"type:bigint;not null"`
	RewardTitle      string  `json:"-" gorm:"type:varchar(255);not null"`
	RewardWalletType string  `json:"-" gorm:"type:varchar(32);not null"`
	IsPity           bool    `json:"-" gorm:"not null"`
	GuaranteeType    string  `json:"-" gorm:"type:varchar(24);not null"`
	Status           string  `json:"status" gorm:"type:varchar(24);not null;index:idx_balance_box_owner_status,priority:2"`
	OpenRecordId     int     `json:"open_record_id,omitempty" gorm:"index"`
	CreatedAt        int64   `json:"created_at" gorm:"type:bigint;not null;index"`
	UpdatedAt        int64   `json:"updated_at" gorm:"type:bigint;not null"`
	OpenedAt         int64   `json:"opened_at,omitempty" gorm:"type:bigint"`
}

// BalanceBlindBoxGift records one ownership transfer batch.
type BalanceBlindBoxGift struct {
	Id                         int    `json:"id"`
	RequestId                  string `json:"request_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	SenderUserId               int    `json:"-" gorm:"not null;index"`
	RecipientUserId            int    `json:"-" gorm:"not null;index"`
	SenderExternalId           string `json:"sender_external_id" gorm:"type:varchar(16);not null"`
	RecipientExternalId        string `json:"recipient_external_id" gorm:"type:varchar(16);not null"`
	SenderDisplayNameMasked    string `json:"sender_display_name_masked" gorm:"type:varchar(64);not null"`
	RecipientDisplayNameMasked string `json:"recipient_display_name_masked" gorm:"type:varchar(64);not null"`
	Quantity                   int    `json:"quantity" gorm:"not null"`
	Status                     string `json:"status" gorm:"type:varchar(24);not null"`
	CreatedAt                  int64  `json:"created_at" gorm:"type:bigint;not null;index"`
}

// BalanceBlindBoxGiftItem preserves the ownership chain for each box.
type BalanceBlindBoxGiftItem struct {
	Id         int   `json:"id"`
	GiftId     int   `json:"gift_id" gorm:"not null;uniqueIndex:idx_balance_box_gift_item"`
	ItemId     int   `json:"item_id" gorm:"not null;uniqueIndex:idx_balance_box_gift_item;index"`
	FromUserId int   `json:"-" gorm:"not null;index"`
	ToUserId   int   `json:"-" gorm:"not null;index"`
	CreatedAt  int64 `json:"created_at" gorm:"type:bigint;not null"`
}

func (p *BalanceBlindBoxPurchase) BeforeCreate(_ *gorm.DB) error {
	p.CreatedAt = platformruntime.GetTimestamp()
	if p.Status == "" {
		p.Status = BalanceBlindBoxPurchaseCompleted
	}
	return nil
}

func (i *BalanceBlindBoxItem) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	i.CreatedAt, i.UpdatedAt = now, now
	if i.Status == "" {
		i.Status = BalanceBlindBoxItemStatusAvailable
	}
	return nil
}

func (g *BalanceBlindBoxGift) BeforeCreate(_ *gorm.DB) error {
	g.CreatedAt = platformruntime.GetTimestamp()
	if g.Status == "" {
		g.Status = BalanceBlindBoxGiftCompleted
	}
	return nil
}

func (i *BalanceBlindBoxGiftItem) BeforeCreate(_ *gorm.DB) error {
	i.CreatedAt = platformruntime.GetTimestamp()
	return nil
}
