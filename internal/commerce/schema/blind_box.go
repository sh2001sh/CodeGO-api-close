package schema

import (
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	BlindBoxRewardTypeQuota        = "quota"
	BlindBoxRewardTypeClaudeQuota  = "claude_quota"
	BlindBoxRewardTypeProp         = "prop"
	BlindBoxRewardTypeSubscription = "subscription"

	BlindBoxOrderSourcePurchase            = "purchase"
	BlindBoxOrderSourceAdminGrant          = "admin_grant"
	BlindBoxOrderSourceSubscriptionBenefit = "subscription_benefit"

	BlindBoxCreditStatusActive    = "active"
	BlindBoxCreditStatusExhausted = "exhausted"
)

type BlindBoxOrder struct {
	Id int `json:"id"`

	UserId      int     `json:"user_id" gorm:"index"`
	Quantity    int     `json:"quantity"`
	OpenedCount int     `json:"opened_count"`
	Money       float64 `json:"money"`

	TradeNo            string `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod      string `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider    string `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	Source             string `json:"source" gorm:"type:varchar(32);default:'purchase';index"`
	UserSubscriptionId int    `json:"user_subscription_id" gorm:"index"`
	BenefitCycle       string `json:"benefit_cycle" gorm:"type:varchar(128);index"`
	ExpiresAt          int64  `json:"expires_at" gorm:"bigint;index"`
	Status             string `json:"status" gorm:"type:varchar(32);index"`
	CreateTime         int64  `json:"create_time" gorm:"index"`
	CompleteTime       int64  `json:"complete_time"`

	ProviderPayload string `json:"provider_payload" gorm:"type:text"`
}

type BlindBoxGrant struct {
	Id              int    `json:"id"`
	UserId          int    `json:"user_id" gorm:"index"`
	AdminUserId     int    `json:"admin_user_id" gorm:"index"`
	BlindBoxOrderId int    `json:"blind_box_order_id" gorm:"index"`
	Quantity        int    `json:"quantity"`
	Reason          string `json:"reason" gorm:"type:varchar(255)"`
	IdempotencyKey  string `json:"-" gorm:"type:varchar(128);uniqueIndex:idx_blind_box_grants_idempotency"`
	TradeNo         string `json:"trade_no" gorm:"type:varchar(255);uniqueIndex"`
	CreatedAt       int64  `json:"created_at" gorm:"bigint;index"`
}

type BlindBoxCredit struct {
	Id int `json:"id"`

	UserId             int     `json:"user_id" gorm:"index"`
	OpenRecordId       int     `json:"open_record_id" gorm:"index"`
	OriginalAmount     int64   `json:"original_amount" gorm:"type:bigint;not null;default:0"`
	RemainingAmount    int64   `json:"remaining_amount" gorm:"type:bigint;not null;default:0;index"`
	RewardUSD          float64 `json:"reward_usd"`
	ExpiresAt          int64   `json:"expires_at" gorm:"bigint;index"`
	Status             string  `json:"status" gorm:"type:varchar(32);index"`
	MigratedAt         int64   `json:"migrated_at" gorm:"bigint;index;default:0"`
	MigratedWalletType string  `json:"migrated_wallet_type" gorm:"type:varchar(32);default:''"`
	CreatedAt          int64   `json:"created_at" gorm:"bigint"`
	UpdatedAt          int64   `json:"updated_at" gorm:"bigint"`
}

type BlindBoxRewardWalletType string

const (
	BlindBoxRewardWalletTypeDefault BlindBoxRewardWalletType = "default"
	BlindBoxRewardWalletTypeClaude  BlindBoxRewardWalletType = "claude"
)

type BlindBoxOpenRecord struct {
	Id int `json:"id"`

	UserId             int     `json:"user_id" gorm:"index"`
	OrderId            int     `json:"order_id" gorm:"index"`
	RewardType         string  `json:"reward_type" gorm:"type:varchar(32);index"`
	RewardWalletType   string  `json:"reward_wallet_type" gorm:"type:varchar(32);default:'default';index"`
	RewardUSD          float64 `json:"reward_usd"`
	CreditAmount       int64   `json:"credit_amount" gorm:"type:bigint;not null;default:0"`
	RewardTitle        string  `json:"reward_title" gorm:"type:varchar(255)"`
	RewardTier         string  `json:"reward_tier" gorm:"type:varchar(64)"`
	UserSubscriptionId int     `json:"user_subscription_id" gorm:"index"`
	IsPity             bool    `json:"is_pity"`
	CreateTime         int64   `json:"create_time" gorm:"bigint;index"`

	PropId        int    `json:"prop_id,omitempty" gorm:"-"`
	PropType      string `json:"prop_type,omitempty" gorm:"-"`
	PropStatus    string `json:"prop_status,omitempty" gorm:"-"`
	PropExpiresAt int64  `json:"prop_expires_at,omitempty" gorm:"-"`
}

type BlindBoxPityState struct {
	Id                    int   `json:"id"`
	UserId                int   `json:"user_id" gorm:"uniqueIndex"`
	ConsecutiveLowRewards int   `json:"consecutive_low_rewards"`
	UpdatedAt             int64 `json:"updated_at" gorm:"bigint"`
}

func (c *BlindBoxCredit) BeforeCreate(tx *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	c.CreatedAt = now
	c.UpdatedAt = now
	return nil
}

func (c *BlindBoxCredit) BeforeUpdate(tx *gorm.DB) error {
	c.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}

func (p *BlindBoxPityState) BeforeCreate(tx *gorm.DB) error {
	p.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}

func (p *BlindBoxPityState) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}
