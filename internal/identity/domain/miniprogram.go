package domain

import (
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	MiniProgramBindingStatusActive  = "active"
	MiniProgramBindingStatusRevoked = "revoked"
)

// UserWeChatBinding stores a website user binding to a mini-program OpenID.
type UserWeChatBinding struct {
	Id         int    `json:"id"`
	UserId     int    `json:"user_id" gorm:"uniqueIndex"`
	OpenID     string `json:"openid" gorm:"column:openid;size:128;uniqueIndex"`
	UnionID    string `json:"unionid" gorm:"column:unionid;size:128;default:''"`
	Status     string `json:"status" gorm:"size:16;index;default:'active'"`
	BoundAt    int64  `json:"bound_at" gorm:"bigint;index"`
	RevokedAt  int64  `json:"revoked_at" gorm:"bigint;default:0"`
	LastSeenAt int64  `json:"last_seen_at" gorm:"bigint;default:0"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt  int64  `json:"updated_at" gorm:"bigint"`
}

func (UserWeChatBinding) TableName() string {
	return "user_wechat_bindings"
}

func (binding *UserWeChatBinding) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	if binding.BoundAt <= 0 && binding.Status == MiniProgramBindingStatusActive {
		binding.BoundAt = now
	}
	if binding.LastSeenAt <= 0 && binding.Status == MiniProgramBindingStatusActive {
		binding.LastSeenAt = now
	}
	if binding.CreatedAt <= 0 {
		binding.CreatedAt = now
	}
	binding.UpdatedAt = now
	return nil
}

func (binding *UserWeChatBinding) BeforeUpdate(_ *gorm.DB) error {
	binding.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}

// MiniProgramBindCode is a short-lived website code used to bind a mini-program account.
type MiniProgramBindCode struct {
	Id           int    `json:"id"`
	UserId       int    `json:"user_id" gorm:"index"`
	CodeHash     string `json:"code_hash" gorm:"size:64;index"`
	ExpiresAt    int64  `json:"expires_at" gorm:"bigint;index"`
	UsedAt       int64  `json:"used_at" gorm:"bigint;default:0"`
	CreatedIP    string `json:"created_ip" gorm:"size:64;default:''"`
	AttemptCount int    `json:"attempt_count" gorm:"default:0"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt    int64  `json:"updated_at" gorm:"bigint"`
}

func (MiniProgramBindCode) TableName() string {
	return "miniprogram_bind_codes"
}

func (code *MiniProgramBindCode) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	if code.CreatedAt <= 0 {
		code.CreatedAt = now
	}
	code.UpdatedAt = now
	return nil
}

func (code *MiniProgramBindCode) BeforeUpdate(_ *gorm.DB) error {
	code.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}
