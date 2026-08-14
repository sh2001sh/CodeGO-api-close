package schema

import (
	"errors"

	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const WalletTransferStatusCompleted = "completed"

var (
	ErrWalletTransferInvalid              = errors.New("额度转账参数无效")
	ErrWalletTransferRecipientNotFound    = errors.New("未找到可接收额度的用户")
	ErrWalletTransferSelf                 = errors.New("不能向自己转账")
	ErrWalletTransferPasswordNotSet       = errors.New("请先设置支付密码")
	ErrWalletTransferPasswordIncorrect    = errors.New("支付密码错误")
	ErrWalletTransferPasswordLocked       = errors.New("支付密码已临时锁定")
	ErrWalletTransferInsufficientBalance  = errors.New("普通额度余额不足")
	ErrWalletTransferAccountPassword      = errors.New("当前登录密码错误")
	ErrWalletTransferPasswordConfirmation = errors.New("两次输入的支付密码不一致")
)

// WalletTransferSecurity stores the independent payment-password credential.
type WalletTransferSecurity struct {
	UserId         int    `json:"-" gorm:"primaryKey;not null"`
	PasswordHash   string `json:"-" gorm:"type:varchar(255);not null"`
	FailedAttempts int    `json:"failed_attempts" gorm:"not null;default:0"`
	LockedUntil    int64  `json:"locked_until" gorm:"type:bigint;not null;default:0"`
	CreatedAt      int64  `json:"created_at" gorm:"type:bigint;not null"`
	UpdatedAt      int64  `json:"updated_at" gorm:"type:bigint;not null"`
}

func (s *WalletTransferSecurity) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	if s.CreatedAt <= 0 {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	return nil
}

func (s *WalletTransferSecurity) BeforeUpdate(_ *gorm.DB) error {
	s.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}

// WalletTransfer is the immutable record for one completed peer transfer.
type WalletTransfer struct {
	Id                         int    `json:"id"`
	RequestId                  string `json:"request_id" gorm:"type:varchar(128);not null;uniqueIndex"`
	SenderUserId               int    `json:"-" gorm:"not null;index:idx_wallet_transfers_sender_created,priority:1"`
	RecipientUserId            int    `json:"-" gorm:"not null;index:idx_wallet_transfers_recipient_created,priority:1"`
	SenderExternalId           string `json:"sender_external_id" gorm:"type:varchar(16);not null"`
	RecipientExternalId        string `json:"recipient_external_id" gorm:"type:varchar(16);not null"`
	SenderDisplayNameMasked    string `json:"sender_display_name_masked" gorm:"type:varchar(64);not null"`
	RecipientDisplayNameMasked string `json:"recipient_display_name_masked" gorm:"type:varchar(64);not null"`
	AmountQuota                int64  `json:"amount_quota" gorm:"type:bigint;not null"`
	SenderBalanceAfter         int64  `json:"sender_balance_after" gorm:"type:bigint;not null"`
	RecipientBalanceAfter      int64  `json:"recipient_balance_after" gorm:"type:bigint;not null"`
	Status                     string `json:"status" gorm:"type:varchar(32);not null;default:'completed'"`
	CreatedAt                  int64  `json:"created_at" gorm:"type:bigint;not null;index:idx_wallet_transfers_sender_created,priority:2;index:idx_wallet_transfers_recipient_created,priority:2"`
}

func (t *WalletTransfer) BeforeCreate(_ *gorm.DB) error {
	if t.Status == "" {
		t.Status = WalletTransferStatusCompleted
	}
	if t.CreatedAt <= 0 {
		t.CreatedAt = platformruntime.GetTimestamp()
	}
	return nil
}
