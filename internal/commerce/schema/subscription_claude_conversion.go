package schema

import (
	"errors"

	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	SubscriptionClaudeConversionStatusCompleted = "completed"

	SubscriptionClaudeConversionEnabledOptionKey = "SubscriptionClaudeConversionEnabled"
)

var (
	SubscriptionClaudeConversionEnabled = true

	ErrSubscriptionClaudeConversionDisabled   = errors.New("月卡转通用额度未开启")
	ErrSubscriptionClaudeConversionInvalid    = errors.New("月卡转换请求无效")
	ErrSubscriptionClaudeConversionNoTarget   = errors.New("当前月卡不可转换")
	ErrSubscriptionClaudeConversionInProgress = errors.New("月卡存在待结算请求，请稍后重试")
	ErrSubscriptionClaudeConversionZeroResult = errors.New("当前月卡剩余价值过低，无法得到通用额度")
)

type SubscriptionClaudeConversion struct {
	Id                 int     `json:"id"`
	UserId             int     `json:"user_id" gorm:"index"`
	UserSubscriptionId int     `json:"user_subscription_id" gorm:"index"`
	RequestId          string  `json:"request_id" gorm:"type:varchar(128);not null;uniqueIndex"`
	Status             string  `json:"status" gorm:"type:varchar(32);not null;default:'completed'"`
	SourceQuota        int64   `json:"source_quota" gorm:"type:bigint;not null;default:0"`
	TargetQuota        int     `json:"target_quota" gorm:"column:target_claude_quota;type:int;not null;default:0"`
	PlanPriceAmount    float64 `json:"plan_price_amount" gorm:"type:decimal(10,6);not null;default:0"`
	UnusedRatio        float64 `json:"unused_ratio" gorm:"type:decimal(12,8);not null;default:0"`
	// Deprecated ratio fields are retained only so historical rows remain readable.
	RatioNumerator   int   `json:"-" gorm:"type:int;not null;default:1"`
	RatioDenominator int   `json:"-" gorm:"type:int;not null;default:10"`
	CreatedAt        int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt        int64 `json:"updated_at" gorm:"bigint"`
}

func (c *SubscriptionClaudeConversion) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	c.CreatedAt = now
	c.UpdatedAt = now
	return nil
}

func (c *SubscriptionClaudeConversion) BeforeUpdate(_ *gorm.DB) error {
	c.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}

type SubscriptionClaudeConversionConfig struct {
	Enabled bool `json:"enabled"`
}

type SubscriptionClaudeConversionPreview struct {
	Eligible        bool    `json:"eligible"`
	RemainingQuota  int64   `json:"remaining_quota"`
	PlanPriceAmount float64 `json:"plan_price_amount"`
	UnusedRatio     float64 `json:"unused_ratio"`
	PreviewQuota    int     `json:"preview_quota"`
}

type SubscriptionClaudeConversionResult struct {
	Conversion      *SubscriptionClaudeConversion      `json:"conversion"`
	SubscriptionId  int                                `json:"subscription_id"`
	SourceQuota     int64                              `json:"source_quota"`
	TargetQuota     int                                `json:"target_quota"`
	QuotaAfter      int                                `json:"quota_after"`
	AmountUsedAfter int64                              `json:"amount_used_after"`
	PeriodUsedAfter int64                              `json:"period_used_after"`
	PlanPriceAmount float64                            `json:"plan_price_amount"`
	UnusedRatio     float64                            `json:"unused_ratio"`
	Config          SubscriptionClaudeConversionConfig `json:"config"`
}
