package schema

import (
	"errors"

	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	SubscriptionClaudeConversionStatusCompleted = "completed"

	SubscriptionClaudeConversionEnabledOptionKey          = "SubscriptionClaudeConversionEnabled"
	SubscriptionClaudeConversionRatioNumeratorOptionKey   = "SubscriptionClaudeConversionRatioNumerator"
	SubscriptionClaudeConversionRatioDenominatorOptionKey = "SubscriptionClaudeConversionRatioDenominator"
	SubscriptionClaudeConversionExcludeDayPassOptionKey   = "SubscriptionClaudeConversionExcludeDayPass"
)

var (
	SubscriptionClaudeConversionEnabled          = true
	SubscriptionClaudeConversionRatioNumerator   = 1
	SubscriptionClaudeConversionRatioDenominator = 10
	SubscriptionClaudeConversionExcludeDayPass   = true

	ErrSubscriptionClaudeConversionDisabled      = errors.New("GPT 套餐转通用额度未开启")
	ErrSubscriptionClaudeConversionInvalidAmount = errors.New("转换额度无效")
	ErrSubscriptionClaudeConversionNoTarget      = errors.New("当前订阅不可转换")
	ErrSubscriptionClaudeConversionInsufficient  = errors.New("套餐可转换额度不足")
	ErrSubscriptionClaudeConversionZeroResult    = errors.New("当前转换额度过小，无法得到通用额度")
)

type SubscriptionClaudeConversion struct {
	Id                 int    `json:"id"`
	UserId             int    `json:"user_id" gorm:"index"`
	UserSubscriptionId int    `json:"user_subscription_id" gorm:"index"`
	RequestId          string `json:"request_id" gorm:"type:varchar(128);not null;uniqueIndex"`
	Status             string `json:"status" gorm:"type:varchar(32);not null;default:'completed'"`
	SourceQuota        int64  `json:"source_quota" gorm:"type:bigint;not null;default:0"`
	TargetClaudeQuota  int    `json:"target_claude_quota" gorm:"type:int;not null;default:0"`
	RatioNumerator     int    `json:"ratio_numerator" gorm:"type:int;not null;default:1"`
	RatioDenominator   int    `json:"ratio_denominator" gorm:"type:int;not null;default:10"`
	CreatedAt          int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt          int64  `json:"updated_at" gorm:"bigint"`
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
	Enabled          bool `json:"enabled"`
	RatioNumerator   int  `json:"ratio_numerator"`
	RatioDenominator int  `json:"ratio_denominator"`
	ExcludeDayPass   bool `json:"exclude_day_pass"`
}

type SubscriptionClaudeConversionPreview struct {
	Eligible           bool  `json:"eligible"`
	MaxSourceQuota     int64 `json:"max_source_quota"`
	PreviewClaudeQuota int   `json:"preview_claude_quota"`
}

type SubscriptionClaudeConversionResult struct {
	Conversion        *SubscriptionClaudeConversion      `json:"conversion"`
	SubscriptionId    int                                `json:"subscription_id"`
	SourceQuota       int64                              `json:"source_quota"`
	TargetClaudeQuota int                                `json:"target_claude_quota"`
	ClaudeQuotaAfter  int                                `json:"claude_quota_after"`
	AmountUsedAfter   int64                              `json:"amount_used_after"`
	PeriodUsedAfter   int64                              `json:"period_used_after"`
	Config            SubscriptionClaudeConversionConfig `json:"config"`
}
