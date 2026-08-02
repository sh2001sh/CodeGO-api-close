package schema

import (
	"errors"

	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	WalletQuotaConversionStandardToClaude = "standard_to_claude"
	WalletQuotaConversionClaudeToStandard = "claude_to_standard"
	WalletQuotaConversionStatusCompleted  = "completed"
	WalletQuotaStandardPerClaude          = int64(4)
)

var (
	ErrWalletQuotaConversionInvalid      = errors.New("额度转换参数无效")
	ErrWalletQuotaConversionInsufficient = errors.New("可转换额度不足")
	ErrWalletQuotaConversionInexact      = errors.New("普通额度转 Claude 额度必须满足 4:1 的完整比例")
)

// WalletQuotaConversion records one completed transfer between wallet pools.
type WalletQuotaConversion struct {
	Id                  int    `json:"id"`
	UserId              int    `json:"user_id" gorm:"index;not null"`
	RequestId           string `json:"request_id" gorm:"type:varchar(128);not null;uniqueIndex"`
	Direction           string `json:"direction" gorm:"type:varchar(32);not null;index"`
	Status              string `json:"status" gorm:"type:varchar(32);not null;default:'completed'"`
	SourceQuota         int64  `json:"source_quota" gorm:"type:bigint;not null"`
	TargetQuota         int64  `json:"target_quota" gorm:"type:bigint;not null"`
	StandardQuotaBefore int64  `json:"standard_quota_before" gorm:"type:bigint;not null"`
	StandardQuotaAfter  int64  `json:"standard_quota_after" gorm:"type:bigint;not null"`
	ClaudeQuotaBefore   int64  `json:"claude_quota_before" gorm:"type:bigint;not null"`
	ClaudeQuotaAfter    int64  `json:"claude_quota_after" gorm:"type:bigint;not null"`
	CreatedAt           int64  `json:"created_at" gorm:"type:bigint;index"`
}

func (c *WalletQuotaConversion) BeforeCreate(_ *gorm.DB) error {
	if c.CreatedAt <= 0 {
		c.CreatedAt = platformruntime.GetTimestamp()
	}
	if c.Status == "" {
		c.Status = WalletQuotaConversionStatusCompleted
	}
	return nil
}
